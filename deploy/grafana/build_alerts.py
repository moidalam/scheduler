#!/usr/bin/env python3
"""Generate the Scheduler alert rules for Grafana.

Rules are code, so thresholds and windows are reviewed like any other change.
Regenerate after editing, then commit the YAML it writes:

    python3 deploy/grafana/build_alerts.py

Each rule links to a runbook in deploy/grafana/runbooks/. Grafana loads the file
from its alerting provisioning directory at startup (see docker-compose.yml).
"""
import json
import pathlib

HERE = pathlib.Path(__file__).parent
OUT = HERE / "alerting" / "scheduler-alerts.yaml"
PROM = {"type": "prometheus", "uid": "prometheus"}
DASHBOARD = "http://localhost:3000/d/scheduler-overview"
ERR = '(sum(increase(operation_total{layer="handler",result="error"}[%s])) or vector(0))'
ALL = 'sum(increase(operation_total{layer="handler"}[%s]))'


def ratio(window):
    return f"({ERR % window}) / ({ALL % window})"


def query(refid, expr, seconds):
    """A Prometheus instant query. relativeTimeRange tells Grafana how far back to look."""
    return {"refId": refid, "relativeTimeRange": {"from": seconds, "to": 0}, "datasourceUid": "prometheus",
            "model": {"refId": refid, "expr": expr, "instant": True, "range": False,
                      "datasource": PROM, "intervalMs": 1000, "maxDataPoints": 43200}}


def threshold(refid, source, op, value):
    """Fires when the source query is above (gt) the value. The condition is the output of this step."""
    return {"refId": refid, "relativeTimeRange": {"from": 0, "to": 0}, "datasourceUid": "__expr__",
            "model": {"refId": refid, "type": "threshold", "expression": source,
                      "conditions": [{"evaluator": {"type": op, "params": [value]},
                                      "operator": {"type": "and"}, "query": {"params": [refid]},
                                      "reducer": {"type": "last"}}]}}


def rule(uid, title, expr, op, value, window_s, for_s, severity, summary, runbook, description):
    return {
        "uid": uid, "title": title, "condition": "C",
        "data": [query("A", expr, window_s), threshold("C", "A", op, value)],
        "noDataState": "OK",          # no traffic is not an alert; the telemetry-gap rule covers that
        "execErrState": "Error",      # a broken query must be visible, not silent
        "for": f"{for_s}s",
        "labels": {"severity": severity, "service": "scheduler"},
        "annotations": {"summary": summary, "description": description,
                        "runbook": f"deploy/grafana/runbooks/{runbook}.md",
                        "dashboard": DASHBOARD},
    }


GROUPS = [
    {"name": "scheduler-slo", "interval": "1m", "rules": [
        rule("scheduler-slo-fast-burn", "SLO fast burn: budget gone in about 2 days",
             f"({ratio('1h')}) / 0.005 > 14.4 and ({ratio('5m')}) / 0.005 > 14.4 ",
             "gt", 0, 3600, 120, "critical",
             "Error budget is burning 14x faster than allowed, over both 1h and 5m.",
             "slo-fast-burn",
             "The 5xx ratio is far above the 0.5% the 99.5% target allows. Both windows agree, so it is not a blip."),
        rule("scheduler-slo-slow-burn", "SLO slow burn: budget gone in about 5 days",
             f"({ratio('6h')}) / 0.005 > 6 and ({ratio('30m')}) / 0.005 > 6",
             "gt", 0, 21600, 600, "warning",
             "Error budget is burning 6x faster than allowed, over both 6h and 30m.",
             "slo-slow-burn",
             "A steady leak of 5xx responses. Not urgent alone, but it spends the monthly budget."),
    ]},
    {"name": "scheduler-errors", "interval": "1m", "rules": [
        rule("scheduler-5xx", "Our failures (5xx) on an operation",
             'sum by (op) (increase(operation_total{layer="handler",result="error"}[5m])) > 0',
             "gt", 0, 300, 300, "warning",
             "{{ $labels.op }} returned 5xx responses in the last 5 minutes.",
             "5xx",
             "Any 5xx is our failure. The labels say which operation; the error_code and the logs say why."),
        rule("scheduler-db-errors", "Database errors in the repository",
             'sum by (op, sqlstate) (increase(operation_total{layer="repository",result="error"}[5m])) > 0',
             "gt", 0, 300, 300, "critical",
             "{{ $labels.op }} failed in the database ({{ $labels.sqlstate }}) in the last 5 minutes.",
             "db-errors",
             "Queries are failing, not just returning no rows. sqlstate says which class: deadlock, statement_timeout, other."),
    ]},
    {"name": "scheduler-latency", "interval": "1m", "rules": [
        rule("scheduler-latency-p95", "p95 latency high on an operation",
             'histogram_quantile(0.95, sum by (le, op) (rate(operation_duration_milliseconds_bucket{layer="handler"}[5m]))) > 500',
             "gt", 0, 300, 600, "warning",
             "{{ $labels.op }} p95 is above 500 ms for 10 minutes.",
             "latency-p95",
             "Normal traffic here is single-digit milliseconds, so 500 ms is a real slowdown. Check the layer split before the cause."),
    ]},
    {"name": "scheduler-bookings", "interval": "1m", "rules": [
        rule("scheduler-conflict-spike", "Booking conflicts spiking",
             'sum(increase(operation_total{layer="handler",op="CreateBooking",result="conflict"}[15m])) / sum(increase(operation_total{layer="handler",op="CreateBooking"}[15m])) > 0.10 and sum(increase(operation_total{layer="handler",op="CreateBooking"}[15m])) > 20',
             "gt", 0, 900, 300, "warning",
             "More than 10% of booking attempts found the slot taken, over at least 20 attempts.",
             "conflict-spike",
             "Clients are booking from stale availability. The service is correct; the client view is out of date."),
    ]},
    {"name": "scheduler-telemetry", "interval": "1m", "rules": [
        rule("scheduler-telemetry-gap", "No metrics from the service",
             'absent_over_time(operation_total{layer="handler"}[10m])',
             "gt", 0, 600, 300, "critical",
             "The service has sent no request metrics for 10 minutes.",
             "telemetry-gap",
             "Either the service is down, or it cannot reach the collector. Both mean the other alerts are blind."),
    ]},
]

doc = {
    "apiVersion": 1,
    "groups": [{"orgId": 1, "name": g["name"], "folder": "Scheduler", "interval": g["interval"], "rules": g["rules"]}
               for g in GROUPS],
}

header = ("# Generated by deploy/grafana/build_alerts.py. Do not edit by hand.\n"
          "# This is YAML written as JSON, which Grafana's provisioning loader accepts.\n")
OUT.write_text(header + json.dumps(doc, indent=2) + "\n")
print(f"wrote {OUT} ({sum(len(g['rules']) for g in GROUPS)} rules)")
