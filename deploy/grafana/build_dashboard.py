#!/usr/bin/env python3
"""Generate the Scheduler Grafana dashboard JSON.

The dashboard is code, so its layout, queries and thresholds are reviewed like
any other change. Regenerate after editing, then commit the JSON:

    python3 deploy/grafana/build_dashboard.py

Grafana loads dashboards/*.json at startup (see scheduler-provider.yaml).
"""
import json
import pathlib

PROM = {"type": "prometheus", "uid": "prometheus"}
OUT = pathlib.Path(__file__).parent / "dashboards" / "scheduler-overview.json"
OPS = 'op=~"$op"'          # filters edge panels by the Operation variable
RATE = "[$__rate_interval]"
RANGE = "[$__range]"

panels = []
_ids = iter(range(1, 1000))
_cur = {"x": 0, "y": 0, "line": 0}


def place(panel, w, h):
    """Left to right in 24 columns, wrapping to a new line when full."""
    if _cur["x"] + w > 24:
        _cur["x"], _cur["y"], _cur["line"] = 0, _cur["y"] + _cur["line"], 0
    panel["id"] = next(_ids)
    panel["gridPos"] = {"x": _cur["x"], "y": _cur["y"], "w": w, "h": h}
    panels.append(panel)
    _cur["x"] += w
    _cur["line"] = max(_cur["line"], h)


def heading(title):
    if _cur["x"] > 0:
        _cur["y"] += _cur["line"]
    _cur["x"], _cur["line"] = 0, 0
    panels.append({"id": next(_ids), "type": "row", "title": title, "collapsed": False,
                   "gridPos": {"x": 0, "y": _cur["y"], "w": 24, "h": 1}, "panels": []})
    _cur["y"] += 1


def q(expr, legend="", instant=False, ref="A"):
    return {"datasource": PROM, "expr": expr, "legendFormat": legend, "refId": ref,
            "range": not instant, "instant": instant}


def ts(title, desc, targets, unit="short", stack=False):
    return {"type": "timeseries", "title": title, "description": desc, "datasource": PROM, "targets": targets,
            "fieldConfig": {"defaults": {"unit": unit, "custom": {
                "drawStyle": "line", "lineWidth": 2, "fillOpacity": 25 if stack else 0,
                "stacking": {"mode": "normal" if stack else "none"}, "showPoints": "never"}}, "overrides": []},
            "options": {"legend": {"displayMode": "list", "placement": "bottom"},
                        "tooltip": {"mode": "multi", "sort": "desc"}}}


def stat(title, desc, expr, unit="short", steps=None, decimals=None):
    defaults = {"unit": unit, "thresholds": {"mode": "absolute", "steps": steps or [{"color": "green", "value": None}]}}
    if decimals is not None:
        defaults["decimals"] = decimals
    return {"type": "stat", "title": title, "description": desc, "datasource": PROM, "targets": [q(expr, instant=True)],
            "fieldConfig": {"defaults": defaults, "overrides": []},
            "options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                        "colorMode": "value", "graphMode": "area", "textMode": "auto"}}


def pie(title, desc, targets):
    return {"type": "piechart", "title": title, "description": desc, "datasource": PROM, "targets": targets,
            "fieldConfig": {"defaults": {"unit": "ms"}, "overrides": []},
            "options": {"pieType": "donut", "displayLabels": ["percent"],
                        "legend": {"displayMode": "table", "placement": "right", "values": ["value", "percent"]},
                        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}}}


def table(title, desc, expr):
    return {"type": "table", "title": title, "description": desc, "datasource": PROM,
            "targets": [q(expr, instant=True)], "fieldConfig": {"defaults": {}, "overrides": []},
            "options": {"showHeader": True, "sortBy": [{"displayName": "Value", "desc": True}]},
            "transformations": [{"id": "organize", "options": {"excludeByName": {"Time": True}}}]}



LOKI = {"type": "loki", "uid": "loki"}
GRAFANA_ANNOTATIONS = {"type": "grafana", "uid": "-- Grafana --"}


def bars(title, desc, targets, unit="short"):
    """Horizontal bars, one per stage of a funnel."""
    return {"type": "bargauge", "title": title, "description": desc, "datasource": PROM, "targets": targets,
            "fieldConfig": {"defaults": {"unit": unit, "decimals": 0,
                            "thresholds": {"mode": "absolute", "steps": [{"color": "blue", "value": None}]}}, "overrides": []},
            "options": {"orientation": "horizontal", "displayMode": "gradient", "showUnfilled": True,
                        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}}}


def loki_ts(title, desc, expr, legend="{{error_code}}", unit="short", stack=False):
    return {"type": "timeseries", "title": title, "description": desc, "datasource": LOKI,
            "targets": [{"datasource": LOKI, "expr": expr, "legendFormat": legend, "refId": "A", "queryType": "range"}],
            "fieldConfig": {"defaults": {"unit": unit, "custom": {"fillOpacity": 25 if stack else 0,
                            "stacking": {"mode": "normal" if stack else "none"}, "showPoints": "never"}}, "overrides": []},
            "options": {"legend": {"displayMode": "list", "placement": "bottom"}, "tooltip": {"mode": "multi", "sort": "desc"}}}


def loki_logs(title, desc, expr):
    return {"type": "logs", "title": title, "description": desc, "datasource": LOKI,
            "targets": [{"datasource": LOKI, "expr": expr, "refId": "A", "queryType": "range"}],
            "options": {"showTime": True, "wrapLogMessage": True, "sortOrder": "Descending", "enableLogDetails": True}}

RED = {"color": "red", "value": None}
ORANGE, GREEN = "orange", "green"

# ---------------------------------------------------------------- overview --
heading("Overview: the numbers to read first")
place(stat("Requests", "Requests the edge served in the selected range.",
           f'sum(increase(operation_total{{layer="handler",{OPS}}}{RANGE}))', decimals=0), 4, 4)
place(stat("Success (2xx/3xx)", "Share of requests answered 2xx or 3xx. Expected 4xx rejections count against this, so it is informational and not coloured as a failure. The failure SLO is in the 5xx rate and the SLO row.",
           f'sum(increase(operation_total{{layer="handler",result="success",{OPS}}}{RANGE})) / sum(increase(operation_total{{layer="handler",{OPS}}}{RANGE})) * 100',
           "percent"), 4, 4)
place(stat("5xx rate", "Share of requests that were our failure. Expected 4xx outcomes are not errors.",
           # "or vector(0)": with no errors the series is absent, and the stat should read 0%, not "No data".
           f'(sum(increase(operation_total{{layer="handler",result="error",{OPS}}}{RANGE})) or vector(0)) / sum(increase(operation_total{{layer="handler",{OPS}}}{RANGE})) * 100',
           "percent", [{"color": GREEN, "value": None}, {"color": ORANGE, "value": 0.5}, {"color": "red", "value": 2}]), 4, 4)
place(stat("p95 latency", "95th percentile of edge duration across the selected operations.",
           f'histogram_quantile(0.95, sum by (le) (increase(operation_duration_milliseconds_bucket{{layer="handler",{OPS}}}{RANGE})))',
           "ms", [{"color": GREEN, "value": None}, {"color": ORANGE, "value": 200}, {"color": "red", "value": 1000}]), 4, 4)
place(stat("Conflicts", "Share of booking attempts that found the slot already taken. A high value means clients act on stale availability.",
           'sum(increase(operation_total{layer="handler",op="CreateBooking",result="conflict"}[$__range])) / sum(increase(operation_total{layer="handler",op="CreateBooking"}[$__range])) * 100',
           "percent", [{"color": GREEN, "value": None}, {"color": ORANGE, "value": 2}, {"color": "red", "value": 10}]), 4, 4)
place(stat("Bookings", "Confirmed bookings in the selected range.",
           'sum(increase(operation_total{layer="handler",op="CreateBooking",result="success"}[$__range]))', decimals=0), 4, 4)

# --------------------------------------------------------------------- SLO --
heading("SLO: availability and error budget (99.5% of requests not 5xx, 30 days)")
# The budget is the 0.5% of requests allowed to fail. Burn rate is how fast we are
# spending it: 1 means exactly on track, 14.4 means the 30-day budget is gone in
# about two days. Expected 4xx outcomes are not failures here.
# "or vector(0)": with no failures the series is absent, and the SLO should read 100%, not "No data".
_ERR = '(sum(increase(operation_total{layer="handler",result="error"}[%s])) or vector(0))'
_ALL = 'sum(increase(operation_total{layer="handler"}[%s]))'
def _ratio(window):
    return f'({_ERR % window}) / ({_ALL % window})'
place(stat("Availability (30d)", "Share of requests that were not our failure, over 30 days. Target 99.5%.",
           f'1 - {_ratio("30d")}', "percentunit",
           [{"color": "red", "value": None}, {"color": ORANGE, "value": 0.995}, {"color": GREEN, "value": 0.999}]), 4, 4)
place(stat("Error budget left (30d)", "Share of the 30-day error budget still unspent. Below 0 means the target is missed.",
           f'1 - ({_ratio("30d")}) / 0.005', "percentunit",
           [{"color": "red", "value": None}, {"color": ORANGE, "value": 0.25}, {"color": GREEN, "value": 0.5}]), 4, 4)
place(stat("Burn rate (1h)", "How fast the budget is being spent in the last hour. 1 is on track; 14.4 exhausts it in two days.",
           f'({_ratio("1h")}) / 0.005', "short",
           [{"color": GREEN, "value": None}, {"color": ORANGE, "value": 1}, {"color": "red", "value": 14.4}], decimals=2), 4, 4)
place(stat("Burn rate (6h)", "Burn rate over six hours. Slower and steadier than the one-hour figure.",
           f'({_ratio("6h")}) / 0.005', "short",
           [{"color": GREEN, "value": None}, {"color": ORANGE, "value": 1}, {"color": "red", "value": 6}], decimals=2), 4, 4)
place(ts("Burn rate over time (1h window)", "Burn rate, with 1 as on track and 14.4 as the fast-burn alert threshold.",
         [q(f'({_ratio("1h")}) / 0.005', "burn rate (1h)")], "short"), 8, 4)

# ----------------------------------------------------------------- traffic --
heading("Traffic: what callers ask for")
place(ts("API rate by operation", "Requests per second, per generated operation.",
         [q(f'sum by (op) (rate(operation_total{{layer="handler",{OPS}}}{RATE}))', "{{op}}")], "reqps"), 12, 8)
place(ts("Outcome rate", "Requests per second by outcome. The error series is our failure.",
         [q(f'sum by (result) (rate(operation_total{{layer="handler",{OPS}}}{RATE}))', "{{result}}")], "reqps", stack=True), 12, 8)

# ------------------------------------------------------------------ funnel --
heading("Booking funnel: from looking at slots to a confirmed booking")
place(bars("Funnel, selected range", "How many requests reach each stage. Availability views come before booking attempts; conflicts and rejections are the leakage.",
           [q('sum(increase(operation_total{layer="handler",op="ListAvailability"}[$__range]))', "1. availability views", True, "A"),
            q('sum(increase(operation_total{layer="handler",op="CreateBooking"}[$__range]))', "2. booking attempts", True, "B"),
            q('sum(increase(operation_total{layer="handler",op="CreateBooking",result="conflict"}[$__range]))', "3. slot already taken", True, "C"),
            q('sum(increase(operation_total{layer="handler",op="CreateBooking",result="rejected"}[$__range]))', "4. rule violation", True, "D"),
            q('sum(increase(operation_total{layer="handler",op="CreateBooking",result="success"}[$__range]))', "5. confirmed", True, "E")]), 12, 8)
place(stat("Conversion", "Confirmed bookings per availability view in the selected range: how many people who look at slots end up booking.",
           'sum(increase(operation_total{layer="handler",op="CreateBooking",result="success"}[$__range])) / sum(increase(operation_total{layer="handler",op="ListAvailability"}[$__range])) * 100',
           "percent", decimals=1), 4, 8)
place(stat("Refused", "Share of booking attempts that were refused (conflict or rule violation).",
           'sum(increase(operation_total{layer="handler",op="CreateBooking",result=~"conflict|rejected"}[$__range])) / sum(increase(operation_total{layer="handler",op="CreateBooking"}[$__range])) * 100',
           "percent", [{"color": GREEN, "value": None}, {"color": ORANGE, "value": 5}, {"color": "red", "value": 20}], decimals=1), 4, 8)
place(ts("Booking attempts over time", "Attempts per second, split by outcome.",
         [q('sum by (result) (rate(operation_total{layer="handler",op="CreateBooking"}[$__rate_interval]))', "{{result}}")], "reqps", stack=True), 8, 8)

# ----------------------------------------------------------------- latency --
heading("Latency: how long callers wait (edge, per operation)")
for name, pct in [("p50", 0.50), ("p95", 0.95), ("p99", 0.99)]:
    place(ts(f"{name} latency by operation", f"{int(pct * 100)}th percentile of edge duration per operation.",
             [q(f'histogram_quantile({pct}, sum by (le, op) (rate(operation_duration_milliseconds_bucket{{layer="handler",{OPS}}}{RATE})))', "{{op}}")], "ms"), 8, 8)

# ------------------------------------------------------------------ errors --
heading("Errors: our failures, and the expected outcomes classified")
place(ts("Our failures (5xx) by operation", "Should stay at zero. Any line here needs a look.",
         [q(f'sum by (op) (rate(operation_total{{layer="handler",result="error",{OPS}}}{RATE}))', "{{op}}")], "reqps"), 12, 8)
place(ts("Expected outcomes by error code", "Every 4xx rejection, by its stable error code. Not errors, but the mix shows what clients hit.",
         [q(f'sum by (error_code) (rate(operation_total{{layer="handler",error_code!="",status_family="4xx",{OPS}}}{RATE}))', "{{error_code}}")],
         "reqps", stack=True), 12, 8)
place(table("Rejections in range, by operation and code", "Counts over the selected range, for triage.",
            f'sum by (op, error_code) (increase(operation_total{{layer="handler",error_code!="",{OPS}}}{RANGE}))'), 12, 7)
place(table("Our failures by layer and Postgres class", "Failures inside the controller and repository, by SQLSTATE class (deadlock, exclusion_violation, ...). Empty on a healthy stack.",
            'sum by (layer, op, sqlstate) (increase(operation_total{result="error",layer=~"repository|controller"}[$__range]))'), 12, 7)

# ------------------------------------------------------------------ layers --
heading("Layers: the same request, seen at each layer")
place(ts("Operations per second, by layer", "Each request is counted once per layer it passes through.",
         [q(f'sum by (layer) (rate(operation_total[{RATE[1:-1]}]))', "{{layer}}")], "reqps"), 12, 8)
place(ts("p95 latency, by layer", "The gap between the edge and the controller is HTTP and tenant work. The gap to the repository is controller logic.",
         [q(f'histogram_quantile(0.95, sum by (le, layer) (rate(operation_duration_milliseconds_bucket[{RATE[1:-1]}])))', "{{layer}}")], "ms"), 12, 8)
place(ts("Database operations per second", "Queries by operation.",
         [q(f'sum by (op) (rate(operation_total{{layer="repository"}}[{RATE[1:-1]}]))', "{{op}}")], "reqps"), 12, 8)
place(ts("Database p95 latency by operation", "Latency per query.",
         [q(f'histogram_quantile(0.95, sum by (le, op) (rate(operation_duration_milliseconds_bucket{{layer="repository"}}[{RATE[1:-1]}])))', "{{op}}")], "ms"), 12, 8)

# -------------------------------------------------------------------- cake --
heading("Time breakdown: where the time goes")
place(pie("Where the time goes",
          "Share of time in the selected range, split without double counting: database, controller logic and HTTP overhead. Nested layers are subtracted from their parents.",
          [q('sum(increase(operation_duration_milliseconds_sum{layer="repository"}[$__range]))', "Database", True, "A"),
           q('sum(increase(operation_duration_milliseconds_sum{layer="controller"}[$__range])) - sum(increase(operation_duration_milliseconds_sum{layer="repository"}[$__range]))', "Controller logic", True, "B"),
           q('sum(increase(operation_duration_milliseconds_sum{layer="handler",op!="unrouted"}[$__range])) - sum(increase(operation_duration_milliseconds_sum{layer="controller"}[$__range]))', "HTTP and tenant", True, "C")]),
      8, 9)
place(pie("Database time by query",
          "Which queries the database spends its time on.",
          [q('sum by (op) (increase(operation_duration_milliseconds_sum{layer="repository"}[$__range]))', "{{op}}", True)]), 8, 9)
place(pie("Edge time by operation",
          "Total edge time per operation, including the layers under it.",
          # "unrouted" is excluded: those are probes that never reached an operation.
          [q(f'sum by (op) (increase(operation_duration_milliseconds_sum{{layer="handler",op!="unrouted",{OPS}}}[$__range]))', "{{op}}", True)]), 8, 9)

# ------------------------------------------------------------- rate limit --
heading("Rate Limiting: rejections under load")
place(ts("Rate limit rejections by scope", "429 responses per second: global vs API limits",
         [q('sum by (scope) (rate(operation_total{layer="handler",status_family="429",' + OPS + '}' + RATE + '))', "{{scope}}")],
         "reqps"), 12, 8)
place(stat("Rate limit rejection rate", "Share of requests rejected (429)",
           '(sum(increase(operation_total{layer="handler",status_family="429",' + OPS + '}' + RANGE + ') or vector(0)) / sum(increase(operation_total{layer="handler",' + OPS + '}' + RANGE + ')) * 100',
           "percent", [{"color": "green", "value": None}, {"color": "orange", "value": 0.1}, {"color": "red", "value": 1}], decimals=2), 12, 8)
place(table("Rate limit rejections in range", "429 responses by operation.",
            'sum by (op) (increase(operation_total{layer="handler",status_family="429",' + OPS + '}' + RANGE + '))'), 24, 7)

# ----------------------------------------------------------------- tenant --
heading("Tenant drill-down (from logs): pick a tenant above")
# Tenant is a log field, not a metric label: a tenant label would multiply the
# metric series by the number of tenants (see ADR-0010). Logs are where a single
# tenant is examined.
_T = '{service_name="scheduler"} | tenant_id = "$tenant"'
place(loki_ts("Requests by operation for this tenant", "Edge requests per second for the chosen tenant, by operation.",
              f'sum by (op) (rate({_T} |~ "^request$" [$__auto]))', "{{op}}", "reqps", stack=True), 12, 8)
place(loki_ts("Rejections for this tenant, by error code", "4xx outcomes for the chosen tenant, by stable error code.",
              f'sum by (error_code) (count_over_time({_T} |~ "^request rejected$" [$__auto]))', "{{error_code}}", "short", stack=True), 12, 8)
place(loki_logs("Recent log lines for this tenant", "The raw log, newest first. Each line carries its request and trace id, so it links to the trace in Tempo.",
                f'{_T}'), 24, 10)

templating = {"list": [{
    "name": "tenant", "label": "Tenant", "type": "textbox", "query": "1", "current": {"text": "1", "value": "1"},
    "hide": 0,
}, {
    "name": "op", "label": "Operation", "type": "query", "datasource": PROM,
    "query": {"query": 'label_values(operation_total{layer="handler"}, op)', "refId": "op"},
    "definition": 'label_values(operation_total{layer="handler"}, op)',
    "includeAll": True, "multi": True, "allValue": ".*", "current": {"text": ["All"], "value": ["$__all"]},
    "refresh": 2, "sort": 1,
}]}

dashboard = {
    "uid": "scheduler-overview",
    "title": "Scheduler: overview",
    "description": "Every layer of the scheduler, from the HTTP edge to the database: traffic, latency, error rate with sub-classification, and where the time goes.",
    "tags": ["scheduler"],
    "timezone": "browser",
    "schemaVersion": 39,
    "version": 1,
    "editable": True,
    "refresh": "30s",
    "time": {"from": "now-1h", "to": "now"},
    "panels": panels,
    "templating": templating,
    # Deploy markers: a deployment is written as an annotation tagged "deploy"
    # (see `make deploy-marker`), and it appears as a vertical line on every panel.
    "annotations": {"list": [{
        "name": "Deployments", "datasource": GRAFANA_ANNOTATIONS, "enable": True, "hide": False,
        "iconColor": "purple", "type": "tags", "tags": ["deploy"],
    }]},
}

OUT.write_text(json.dumps(dashboard, indent=2) + "\n")
print(f"wrote {OUT} ({len(panels)} panels)")
