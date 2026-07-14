# openHAB integration

This guide wires PiMonitor's REST API into [openHAB](https://www.openhab.org/)
so that a Raspberry Pi's CPU load, temperature, and memory usage show up as
openHAB Items you can chart, alert on, or display on a sitemap. It uses
openHAB's built-in [HTTP binding](https://www.openhab.org/addons/bindings/http/)
polling PiMonitor's [`/api/v1/metrics`](../API.md#get-apiv1metrics) endpoint —
no extra software on the Pi.

If you would rather scrape PiMonitor into a Prometheus/Grafana stack instead
of (or in addition to) openHAB, skip to
[Prometheus alternative](#prometheus-alternative) below.

## Prerequisites

- PiMonitor running and reachable from your openHAB server. Verify with:

  ```sh
  curl -s http://raspberrypi.local:8080/api/v1/metrics | jq .
  ```

  Replace `raspberrypi.local:8080` with your Pi's address and the port from
  `listen_addr` (default `:8080`) throughout this guide.

- The **HTTP binding** installed in openHAB
  (*Settings → Bindings → + → HTTP Binding*), and the **JSONPATH**
  transformation installed
  (*Settings → Transformations → + → JSONPath*). The binding uses JSONPath to
  pull individual fields out of the single metrics JSON document.

## How it works

PiMonitor returns the whole metrics snapshot as one JSON document. Rather than
polling a separate URL per value, the HTTP binding fetches
`/api/v1/metrics` **once** per refresh cycle (`baseURL` + `refresh`), and each
Channel extracts its own field from that shared response with a
`stateTransformation` (`JSONPATH:$.<field>`). This keeps the request count at
one per interval no matter how many Items you define.

The field paths below map 1:1 onto the response documented in
[`docs/API.md`](../API.md#get-apiv1metrics).

## Things

Create `things/pimonitor.things`:

```java
Thing http:url:pimonitor "PiMonitor" [
    baseURL="http://raspberrypi.local:8080/api/v1/metrics",
    refresh=30
] {
    Channels:
        Type number : cpu           "CPU Usage"          [ stateTransformation="JSONPATH:$.cpu.overall_percent" ]
        Type number : load1         "Load Average (1m)"  [ stateTransformation="JSONPATH:$.load_average.load1" ]
        Type number : temperature   "CPU Temperature"    [ stateTransformation="JSONPATH:$.temperature.celsius" ]
        Type number : gpuTemp       "GPU Temperature"    [ stateTransformation="JSONPATH:$.gpu_temperature.celsius" ]
        Type number : memory        "Memory Used"        [ stateTransformation="JSONPATH:$.memory.used_percent" ]
        Type number : swap          "Swap Used"          [ stateTransformation="JSONPATH:$.swap.used_percent" ]
        Type number : rootDisk      "Root FS Used"       [ stateTransformation="JSONPATH:$.disks[?(@.mountpoint=='/')].used_percent" ]
        Type number : updates       "Available Updates"  [ stateTransformation="JSONPATH:$.updates.count" ]
        Type number : uptime        "Uptime (s)"         [ stateTransformation="JSONPATH:$.uptime_seconds" ]
        Type string : piModel       "Pi Model"           [ stateTransformation="JSONPATH:$.system.pi_model" ]
        Type string : kernel        "Kernel"             [ stateTransformation="JSONPATH:$.system.kernel_version" ]
}
```

Notes:

- `refresh=30` polls every 30 seconds. There is no benefit to polling faster
  than PiMonitor's own `poll_interval_seconds` (default 5); 30–60s is plenty
  for a home dashboard and keeps load on the Pi negligible.
- The `rootDisk` channel uses a JSONPath filter to select the `/` mountpoint
  from the `disks` array. Adjust the mountpoint (e.g. `'/mnt/data'`) or add
  more channels for other filesystems.
- `gpu_temperature` is only present when `vcgencmd` is available on the Pi;
  on models/OS images without it, that Channel simply stays undefined.
- All Channels are read-only — PiMonitor's API is a metrics source, so use
  the openHAB Items only for display and rules, not commands.

### With an API key

If you set `api_key` in PiMonitor's config, every `/api/v1/...` request must
carry it. Add a request header to the Thing:

```java
Thing http:url:pimonitor "PiMonitor" [
    baseURL="http://raspberrypi.local:8080/api/v1/metrics",
    refresh=30,
    headers="X-Api-Key=YOUR_API_KEY_HERE"
] {
    // ... Channels as above ...
}
```

## Items

Create `items/pimonitor.items` and link each Item to its Channel:

```java
Number:Dimensionless PiMonitor_CPU         "CPU Usage [%.1f %%]"          <cpu>          { channel="http:url:pimonitor:cpu" }
Number               PiMonitor_Load1       "Load Average 1m [%.2f]"       <chart>        { channel="http:url:pimonitor:load1" }
Number:Temperature   PiMonitor_Temp        "CPU Temperature [%.1f °C]"    <temperature>  { channel="http:url:pimonitor:temperature" }
Number:Temperature   PiMonitor_GPUTemp     "GPU Temperature [%.1f °C]"    <temperature>  { channel="http:url:pimonitor:gpuTemp" }
Number:Dimensionless PiMonitor_Memory      "Memory Used [%.1f %%]"        <memory>       { channel="http:url:pimonitor:memory" }
Number:Dimensionless PiMonitor_Swap        "Swap Used [%.1f %%]"          <memory>       { channel="http:url:pimonitor:swap" }
Number:Dimensionless PiMonitor_RootDisk    "Root FS Used [%.1f %%]"       <harddisk>     { channel="http:url:pimonitor:rootDisk" }
Number               PiMonitor_Updates     "Available Updates [%d]"       <settings>     { channel="http:url:pimonitor:updates" }
Number:Time          PiMonitor_Uptime      "Uptime [%.0f s]"              <time>         { channel="http:url:pimonitor:uptime" }
String               PiMonitor_Model       "Pi Model [%s]"                <network>      { channel="http:url:pimonitor:piModel" }
String               PiMonitor_Kernel      "Kernel [%s]"                  <network>      { channel="http:url:pimonitor:kernel" }
```

> The `Number:Temperature` / `Number:Dimensionless` unit dimensions are
> optional niceties; plain `Number` works too if you prefer not to deal with
> units. The `°C` / `%` in the state description are just display formatting.

## Verify

1. Reload the files (openHAB picks up changes to `things/` and `items/`
   automatically) and open *Settings → Things → PiMonitor*. The Thing should
   go **ONLINE** within one refresh cycle.
2. In *Developer Tools → Items* (or the Main UI Item pages), confirm
   `PiMonitor_CPU`, `PiMonitor_Temp`, and `PiMonitor_Memory` show live values
   that change over time.
3. Add them to a sitemap or Main UI page, e.g.:

   ```java
   sitemap pimonitor label="Raspberry Pi" {
       Frame label="System" {
           Text item=PiMonitor_CPU
           Text item=PiMonitor_Temp
           Text item=PiMonitor_Memory
           Text item=PiMonitor_Updates
       }
   }
   ```

### Troubleshooting

- **Thing stuck OFFLINE / `COMMUNICATION_ERROR`.** The URL is wrong or
  PiMonitor is unreachable from the openHAB host. Re-run the `curl` check
  from the openHAB machine (not your laptop) and confirm the port matches
  `listen_addr`.
- **Thing OFFLINE with HTTP 401.** An `api_key` is configured on PiMonitor
  but the `headers="X-Api-Key=..."` entry is missing or wrong. See
  [With an API key](#with-an-api-key).
- **Items stay `NULL`/`UNDEF`.** The JSONPATH transformation isn't installed,
  or a field path is off. Confirm the path against a live response
  (`curl ... | jq '.cpu.overall_percent'`); remember hardware-specific fields
  like `temperature` and `gpu_temperature` read as empty on non-Pi hosts and
  `gpu_temperature` is omitted entirely without `vcgencmd`.

## Prometheus alternative

PiMonitor does not expose a native Prometheus `/metrics` endpoint — its API is
JSON-only. To scrape it into Prometheus, put the
[`json_exporter`](https://github.com/prometheus-community/json_exporter)
(from prometheus-community) in front of `/api/v1/metrics`; it maps JSON fields
to Prometheus samples.

`json_exporter` config (`json_exporter.yml`):

```yaml
modules:
  pimonitor:
    metrics:
      - name: pimonitor_cpu_percent
        path: '{ .cpu.overall_percent }'
        help: Overall CPU usage percent
      - name: pimonitor_load1
        path: '{ .load_average.load1 }'
        help: 1-minute load average
      - name: pimonitor_temperature_celsius
        path: '{ .temperature.celsius }'
        help: CPU temperature in Celsius
      - name: pimonitor_gpu_temperature_celsius
        path: '{ .gpu_temperature.celsius }'
        help: GPU temperature in Celsius (only present when vcgencmd is available)
      - name: pimonitor_uptime_seconds
        path: '{ .uptime_seconds }'
        help: Seconds since boot
      - name: pimonitor_memory_used_percent
        path: '{ .memory.used_percent }'
        help: Memory used percent
      - name: pimonitor_swap_used_percent
        path: '{ .swap.used_percent }'
        help: Swap used percent
      - name: pimonitor_updates_count
        path: '{ .updates.count }'
        help: Available apt updates
      - name: pimonitor_disk_used_percent
        type: object
        path: '{ .disks[*] }'
        help: Filesystem used percent by mountpoint
        labels:
          mountpoint: '{ .mountpoint }'
        values:
          used_percent: '{ .used_percent }'
      - name: pimonitor_network_bytes_per_sec
        type: object
        path: '{ .network[*] }'
        help: Network throughput per interface (omitted when network monitoring is disabled)
        labels:
          interface: '{ .name }'
        values:
          rx_bytes_per_sec: '{ .rx_bytes_per_sec }'
          tx_bytes_per_sec: '{ .tx_bytes_per_sec }'
    # If PiMonitor has an api_key set:
    # http_client_config:
    #   headers:
    #     X-Api-Key: YOUR_API_KEY_HERE
```

Run the exporter (default port `7979`):

```sh
json_exporter --config.file=json_exporter.yml
```

Then scrape it from Prometheus (`prometheus.yml`). The exporter fetches the
target passed as the `target` URL parameter, so point it at PiMonitor:

```yaml
scrape_configs:
  - job_name: pimonitor
    metrics_path: /probe
    params:
      module: [pimonitor]
    static_configs:
      - targets:
          - http://raspberrypi.local:8080/api/v1/metrics
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: 127.0.0.1:7979   # where json_exporter runs
```

This yields `pimonitor_cpu_percent`, `pimonitor_temperature_celsius`,
`pimonitor_memory_used_percent`, and a per-mountpoint
`pimonitor_disk_used_percent{mountpoint="/"}` in Prometheus, ready to graph in
Grafana or alert on.
