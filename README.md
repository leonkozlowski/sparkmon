# sparkmon

A terminal dashboard for a small **NVIDIA DGX Spark** cluster. It polls
`node_exporter` (host metrics) and NVIDIA `dcgm-exporter` (GPU metrics) on each
node over plain HTTP and renders a unified, side-by-side view — no Prometheus,
no Grafana, no browser.

Built with [`jig`](https://github.com/atterpac/jig) (a `tview`-based TUI
toolkit). Inspired by [paul-aviles/NVIDIA-DGX-Spark-Dashboard](https://github.com/paul-aviles/NVIDIA-DGX-Spark-Dashboard).

```
   ┌──────────── spark-01 ────────────┐  ┌──────────── spark-02 ────────────┐
   │ ● up   uptime 3d 4h               │  │ ● up   uptime 3d 4h               │
   │ CPU  37%  · 20 cores · load 1.4 …  │  │ CPU  62%  · 20 cores · load 3.1 … │
   │ CPU %  ▁▂▃▅▇▆▅▄▃▂▃▅▇█▆▄▃▂▁         │  │ CPU %  ▃▅▇█▇▆▇█▇▆▅▄▅▆▇█▇▆▅▄       │
   │ MEM  [█████░░░░░░░░░░] 31%         │  │ MEM  [████████░░░░░░] 58%         │
   │ GPU                               │  │ GPU                               │
   │  gpu0 NVIDIA GB10                  │  │  gpu0 NVIDIA GB10                 │
   │   util  41%  vram 18.3/119.7 GiB …│  │   util  88%  vram 71.2/119.7 GiB …│
   │ net rx ▁▁▂▁▃▂▁  net tx ▁▁▁▂▁▁     │  │ net rx ▂▃▅▃▂▁▂  net tx ▁▂▁▁▁      │
   └───────────────────────────────────┘  └───────────────────────────────────┘
```

## What's where

| Path | What it is |
|---|---|
| `cmd/sparkmon/` | the binary entrypoint (subcommand router) |
| `internal/cli/` | subcommands: `dashboard`, `deploy`, `teardown`, `health`, `version` |
| `internal/config/` | config file + `-nodes` flag parsing |
| `internal/metrics/` | HTTP scrape + Prometheus-text parser + per-node snapshots/rates |
| `internal/ui/` | the `jig` dashboard |
| `internal/exporters/docker-compose.yml` | `node_exporter` + `dcgm-exporter` stack — embedded in the binary, deployed to each Spark node |
| `config.yaml.example` | sample config |
| `Justfile` | `just build`, `just run`, `just deploy-exporters`, … (needs [`just`](https://github.com/casey/just)) |

## Setup

### 1. Run the exporters on each DGX Spark node

These are the only things that need to run *on* the nodes (two small
containers). Prereqs per node: Docker + Compose plugin, and the NVIDIA Container
Toolkit (DGX OS ships with it; otherwise see the
[install guide](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html);
verify with `docker run --rm --gpus all ubuntu nvidia-smi`).

From your workstation:

```bash
sparkmon deploy me@spark-01 me@spark-02
# or: just deploy-exporters "me@spark-01 me@spark-02"
```

This SSHes to each target, uploads the embedded `docker-compose.yml`, pulls the
images, and brings the stack up. The compose file lives at
`internal/exporters/docker-compose.yml` in this repo if you want to deploy by
hand.

Check that the exporters are reachable:

```bash
sparkmon health spark-01 spark-02
```

Tear it down later with `sparkmon teardown me@spark-01 me@spark-02`
(add `--purge` to also remove `~/sparkmon-exporters` on each node).

### 2. Run the dashboard

#### Option A: Install with curl (Recommended)

**One-line install:**

```bash
# Install to /usr/local/bin/sparkmon
curl -fsSL https://raw.githubusercontent.com/leonkozlowski/sparkmon/main/install.sh | bash
# Or to custom location:
curl -fsSL https://raw.githubusercontent.com/leonkozlowski/sparkmon/main/install.sh | bash -s -- --bin-dir ~/bin --install-dir ~/.sparkmon
```

**Quick start:**

```bash
# Copy and edit config
mkdir -p ~/.sparkmon
cp /opt/sparkmon/config.yaml.example ~/.sparkmon/config.yaml
# Edit with your node IPs: nano ~/.sparkmon/config.yaml
# Run the dashboard:
sparkmon -config ~/.sparkmon/config.yaml
```

#### Option B: Build from source

Needs Go 1.24+. Builds anywhere — your laptop, or one of the Spark nodes
(`just build-arm` cross-compiles a `linux/arm64` binary).

```bash
git clone https://github.com/leonkozlowski/sparkmon.git
cd sparkmon

cp config.yaml.example config.yaml       # put your node IPs/hostnames in it
go run ./cmd/sparkmon                    # or: just run   (or: just build && ./bin/sparkmon)
```

No config file? Pass nodes inline:

```bash
go run ./cmd/sparkmon -nodes spark-01=192.168.1.101,spark-02=192.168.1.102
# or: just demo "spark-01=192.168.1.101,spark-02=192.168.1.102"
```

Dashboard flags: `-config <file>` (default `config.yaml`), `-nodes name=host,…`,
`-interval 2s`, `-theme <name>`.

### CLI

`sparkmon` is one binary with subcommands. The dashboard runs by default.

```text
sparkmon                              # dashboard (= sparkmon dashboard)
sparkmon deploy   me@spark-01 ...     # upload + bring up the exporter stack
sparkmon teardown me@spark-01 ...     # stop the stack (--purge removes ~/sparkmon-exporters)
sparkmon health   spark-01 ...        # probe TCP + /metrics on :9100 and :9400
sparkmon version
sparkmon help
```

`sparkmon deploy` and `teardown` shell out to the system `ssh` (so your
`~/.ssh/config`, agent, and known hosts all work as you'd expect).

### Keys

- `r` — refresh now
- `t` — cycle theme (from `jig`)
- `q` / `Ctrl-C` — quit

## What it shows, per node

- **Status line:** up/down, uptime, CPU %, core count, 1-min load, memory
  used/total (%), `/` filesystem %, current disk and network throughput.
- **CPU % sparkline** — derived from `node_cpu_seconds_total` deltas.
- **Memory bar.**
- **GPU block** — per GPU (from `dcgm-exporter`): utilization %, framebuffer
  VRAM used/total, temperature, power draw, SM clock.
- **Net RX / TX sparklines** — summed over physical interfaces.

All nodes are shown together on one screen (one column each), so it works
"cross-Spark" the same way a Prometheus + Grafana setup would — same exporters,
same metrics. The trade-offs vs. that stack: no long-term history (a sparkline
only holds the last `history` points, 60 by default), no alerting, and it's
local to your terminal rather than a shared web UI.

## Config

`sparkmon` looks for a config file in this order; the first one that exists wins:

1. `-config <path>` (explicit override)
2. `$XDG_CONFIG_HOME/sparkmon/config.yaml`
3. `~/.config/sparkmon/config.yaml`  ← recommended
4. `~/.sparkmon/config.yaml`         ← legacy
5. `./config.yaml`                    ← dev convenience

Create one:

```sh
mkdir -p ~/.config/sparkmon
cp config.yaml.example ~/.config/sparkmon/config.yaml
$EDITOR ~/.config/sparkmon/config.yaml
```

Schema:

```yaml
interval: 2s              # poll cadence
history: 60               # points kept per sparkline
theme: tokyonight-night   # optional; any of the 26 built-in jig themes
nodes:
  - name: spark-01
    host: 192.168.1.101
  - name: spark-02
    host: 192.168.1.102
    # node_port: 9100     # override if the exporters aren't on the defaults
    # gpu_port: 9400
```

## Notes

- **DGX Spark is ARM64.** The exporter images (`prom/node-exporter`,
  `nvcr.io/nvidia/k8s/dcgm-exporter`) publish `linux/arm64`, and `just build-arm`
  builds the TUI for the nodes too.
- **`dcgm-exporter` GPU access.** `internal/exporters/docker-compose.yml` uses the Compose
  `deploy.resources.reservations.devices` syntax; swap it for `runtime: nvidia`
  if your Docker is set up the older way.
- **No GPU metrics?** If `dcgm-exporter` can't enumerate the GB10 on your DGX OS
  build, the dashboard just shows "no GPUs reported" for that node and keeps
  working on host metrics — or run an `nvidia-smi`-based exporter and adjust
  `parseGPUs` in `internal/metrics/collect.go`.
- **Firewall.** The host running `sparkmon` must reach `tcp/9100` and `tcp/9400`
  on each node.

## Roadmap ideas

- A per-node drill-down view (full-screen, more panels)
- Threshold coloring + an alerts pane (GPU temp, disk full, node down)
- Optional on-disk history so sparklines survive a restart
- Mouse/scroll for picking a node
