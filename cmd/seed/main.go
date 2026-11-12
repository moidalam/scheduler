// Command seed loads the supplied appointments.json into a default tenant.
//
// The file's integers become the real identifiers (ADR-0008): trainer_id is a
// resource id, user_id a requester id, id a booking id. Rows are inserted with
// explicit ids and each identity sequence is then advanced past them, so new
// rows do not collide. It is idempotent: a second run changes nothing.
//
// Records that break current policy, such as a 60-minute booking, load as-is
// and are reported, not rejected (PRD R-3).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/moidalam/scheduler/internal/config"
	"github.com/moidalam/scheduler/internal/db"
	"github.com/moidalam/scheduler/internal/schedule"
)

type appointment struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	TrainerID int64     `json:"trainer_id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

const (
	serviceID   = 1
	serviceName = "30-minute video check-in"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	file := flag.String("file", "data/appointments.seed.json", "path to appointments.json")
	tenant := flag.Int64("tenant", 1, "tenant id to load into")
	tz := flag.String("timezone", "America/Los_Angeles", "IANA timezone for the trainers")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()

	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var appts []appointment
	if err := json.Unmarshal(raw, &appts); err != nil {
		return fmt.Errorf("parse %s: %w", *file, err)
	}

	if err := db.RunMigrations(ctx, cfg.DatabaseURL, logger); err != nil {
		return err
	}
	pool, err := db.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	inserted := map[string]int64{}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		exec := func(table, sql string, args ...any) error {
			tag, err := tx.Exec(ctx, sql, args...)
			inserted[table] += tag.RowsAffected()
			return err
		}

		if err := exec("tenants", `INSERT INTO scheduler.tenants (id, name) VALUES ($1, 'Default') ON CONFLICT (id) DO NOTHING`, *tenant); err != nil {
			return fmt.Errorf("tenant: %w", err)
		}
		if err := exec("services", `INSERT INTO scheduler.services (tenant_id, id, name) VALUES ($1, $2, $3) ON CONFLICT (tenant_id, id) DO NOTHING`,
			*tenant, serviceID, serviceName); err != nil {
			return fmt.Errorf("service: %w", err)
		}

		trainers, requesters := distinct(appts, func(a appointment) int64 { return a.TrainerID }), distinct(appts, func(a appointment) int64 { return a.UserID })

		for _, id := range trainers {
			if err := exec("resources", `
				INSERT INTO scheduler.resources (tenant_id, id, type, display_name, timezone, default_service_id)
				VALUES ($1, $2, 'trainer', $3, $4, $5) ON CONFLICT (tenant_id, id) DO NOTHING`,
				*tenant, id, fmt.Sprintf("Trainer %d", id), *tz, serviceID); err != nil {
				return fmt.Errorf("resource %d: %w", id, err)
			}
			// Monday to Friday, 08:00-17:00 local. Skipped if the resource has a rule that day.
			for wd := 1; wd <= 5; wd++ {
				if err := exec("availability_rules", `
					INSERT INTO scheduler.availability_rules (tenant_id, resource_id, weekday, starts_at_local, ends_at_local)
					SELECT $1, $2, $3, '08:00', '17:00'
					WHERE NOT EXISTS (
					    SELECT 1 FROM scheduler.availability_rules
					    WHERE tenant_id = $1 AND resource_id = $2 AND weekday = $3)`,
					*tenant, id, wd); err != nil {
					return fmt.Errorf("rule for resource %d: %w", id, err)
				}
			}
		}
		for _, id := range requesters {
			if err := exec("requesters", `INSERT INTO scheduler.requesters (tenant_id, id) VALUES ($1, $2) ON CONFLICT (tenant_id, id) DO NOTHING`, *tenant, id); err != nil {
				return fmt.Errorf("requester %d: %w", id, err)
			}
		}
		for _, a := range appts {
			if err := exec("bookings", `
				INSERT INTO scheduler.bookings (tenant_id, id, resource_id, requester_id, service_id, starts_at, ends_at, metadata)
				VALUES ($1, $2, $3, $4, $5, $6, $7, '{"source":"appointments.json"}')
				ON CONFLICT DO NOTHING`,
				*tenant, a.ID, a.TrainerID, a.UserID, serviceID, a.StartedAt, a.EndedAt); err != nil {
				return fmt.Errorf("booking %d: %w", a.ID, err)
			}
		}

		// Advance each identity sequence past the explicit ids, never backwards,
		// so the next generated id cannot collide with a seeded one.
		for _, table := range []string{"tenants", "services", "resources", "requesters", "bookings"} {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`
				SELECT setval(pg_get_serial_sequence('scheduler.%[1]s', 'id'),
				    GREATEST(
				        COALESCE((SELECT MAX(id) FROM scheduler.%[1]s), 0) + 1,
				        COALESCE((SELECT last_value FROM pg_sequences
				                  WHERE schemaname = 'scheduler' AND sequencename = '%[1]s_id_seq'), 0) + 1),
				    false)`, table)); err != nil {
				return fmt.Errorf("advance %s sequence: %w", table, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	policy := schedule.Policy{DurationMinutes: 30, GranularityMinutes: 30, MaxHorizonDays: 60}
	rules := businessWeek()
	flagged := 0
	for _, a := range appts {
		// Judge each record as if it were being made just before it starts, so
		// only the rules about the booking itself apply, not "is it in the past".
		if v := schedule.ValidateBooking(loc, rules, policy, a.StartedAt, a.EndedAt, a.StartedAt.Add(-time.Minute)); v != nil {
			flagged++
			logger.Warn("seed booking violates current policy; loaded as-is",
				slog.Int64("booking_id", a.ID), slog.String("rule", v.Code),
				slog.Duration("duration", a.EndedAt.Sub(a.StartedAt)))
		}
	}

	logger.Info("seed complete",
		slog.Int("appointments_in_file", len(appts)),
		slog.Int64("tenants_inserted", inserted["tenants"]),
		slog.Int64("resources_inserted", inserted["resources"]),
		slog.Int64("requesters_inserted", inserted["requesters"]),
		slog.Int64("rules_inserted", inserted["availability_rules"]),
		slog.Int64("bookings_inserted", inserted["bookings"]),
		slog.Int("policy_violations_flagged", flagged))
	return nil
}

func businessWeek() []schedule.Rule {
	var rs []schedule.Rule
	for wd := time.Monday; wd <= time.Friday; wd++ {
		rs = append(rs, schedule.Rule{Weekday: wd, StartMinute: 8 * 60, EndMinute: 17 * 60})
	}
	return rs
}

func distinct(appts []appointment, key func(appointment) int64) []int64 {
	seen := map[int64]struct{}{}
	for _, a := range appts {
		seen[key(a)] = struct{}{}
	}
	out := make([]int64, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
