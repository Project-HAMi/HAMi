import json
import argparse
import numpy as np
import matplotlib.pyplot as plt
from pathlib import Path

###############################################
# Config
###############################################

DEFAULT_TRIM_PCT = 5
DEFAULT_MAD_Z = 3.5

###############################################
# Loading
###############################################

def load_requests(path):
    rows = []
    with open(path) as f:
        for line in f:
            rows.append(json.loads(line))
    return rows

def compute_stats(rows):
    ttft = []
    per_token = []

    for r in rows:
        if r["t_first"] is None:
            continue
        ttft.append(r["t_first"] - r["t0"])

        ts = r["t_tokens"]
        if len(ts) > 1:
            diffs = np.diff(ts)
            per_token.extend(diffs)

    ttft = np.array(ttft)
    per_token = np.array(per_token)

    stats = {
        "p50": np.percentile(ttft, 50),
        "p95": np.percentile(ttft, 95),
        "p99": np.percentile(ttft, 99),
    }
    return stats, ttft, per_token


###############################################
# Noise removal
###############################################

def trimmed(x, pct=5):
    lo = np.percentile(x, pct)
    hi = np.percentile(x, 100 - pct)
    return x[(x >= lo) & (x <= hi)]

def mad_filter(x, z=3.5):
    med = np.median(x)
    mad = np.median(np.abs(x - med))
    dev = np.abs(x - med) / (mad * 1.4826 + 1e-9)
    return x[dev < z]

def clean_array(x, trim_pct, mad_z):
    a = trimmed(x, trim_pct)
    a = mad_filter(a, mad_z)
    return a


###############################################
# Plot helpers
###############################################

def fixed_range(data_list, margin=0.05):
    all_data = np.concatenate(data_list)
    lo, hi = np.min(all_data), np.max(all_data)
    span = hi - lo
    return lo - margin * span, hi + margin * span

def plot_hist(datasets, xlabel, title, out):
    xs = [d[1] for d in datasets]
    xmin, xmax = fixed_range(xs)

    plt.figure()
    for name, arr in datasets:
        plt.hist(arr, bins=50, alpha=0.6, label=name)
    plt.xlabel(xlabel)
    plt.title(title)
    plt.legend()
    plt.xlim(xmin, xmax)
    plt.savefig(out)

def plot_cdf(datasets, xlabel, title, out):
    plt.figure()
    for name, arr in datasets:
        arr_sorted = np.sort(arr)
        y = np.linspace(0, 1, len(arr_sorted))
        plt.plot(arr_sorted, y, label=name)
    plt.xlabel(xlabel)
    plt.ylabel("CDF")
    plt.title(title)
    plt.grid(True, linestyle="--", alpha=.3)
    plt.legend()
    plt.savefig(out)

def plot_violin(datasets, xlabel, title, out):
    plt.figure()
    data = [d[1] for d in datasets]
    labels = [d[0] for d in datasets]

    plt.violinplot(data, showmeans=True)
    plt.xticks(np.arange(1, len(labels)+1), labels)
    plt.ylabel(xlabel)
    plt.title(title)
    plt.grid(True, linestyle="--", alpha=.3)
    plt.savefig(out)


###############################################
# Reporting
###############################################

def generate_report(results, outdir):
    lines = []
    lines.append("# LLM Benchmark Clean Report\n")

    for name, r in results.items():
        s = r["stats"]
        c = r["clean"]

        lines.append(f"## {name}")
        lines.append(f"- raw p50: `{s['p50']:.4f}`")
        lines.append(f"- raw p95: `{s['p95']:.4f}`")
        lines.append(f"- raw p99: `{s['p99']:.4f}`")
        lines.append(f"- clean mean TTFT: `{c['ttft_clean_mean']:.4f}`")
        lines.append(f"- clean mean per-token: `{c['pt_clean_mean']:.4f}`\n")

    lines.append("## Figures\n")
    for f in sorted(outdir.iterdir()):
        if f.suffix == ".png":
            lines.append(f"![{f.stem}]({f.name})")

    report_path = outdir / "report.md"
    report_path.write_text("\n".join(lines))
    print("Generated report:", report_path)


###############################################
# Main
###############################################

def main():
    parser = argparse.ArgumentParser(description="Generate benchmark comparison report")
    parser.add_argument("--dataset", action="append", nargs=2, metavar=("NAME", "FILE"),
                        required=True, help="Dataset name and JSONL file (can be repeated)")
    parser.add_argument("--trim-pct", type=float, default=DEFAULT_TRIM_PCT,
                        help="Percentile trim for outlier removal (default: 5)")
    parser.add_argument("--mad-z", type=float, default=DEFAULT_MAD_Z,
                        help="MAD z-score threshold (default: 3.5)")
    parser.add_argument("--output-dir", default="report",
                        help="Output directory (default: report)")
    args = parser.parse_args()

    outdir = Path(args.output_dir)
    outdir.mkdir(exist_ok=True)

    results = {}

    for name, file in args.dataset:
        print(f"Loading {name} from {file}...")
        rows = load_requests(file)
        stats, ttft, pt = compute_stats(rows)

        ttft_clean = clean_array(ttft, args.trim_pct, args.mad_z)
        pt_clean = clean_array(pt, args.trim_pct, args.mad_z)

        results[name] = {
            "stats": stats,
            "ttft": ttft,
            "pt": pt,
            "ttft_clean": ttft_clean,
            "pt_clean": pt_clean,
            "clean": {
                "ttft_clean_mean": ttft_clean.mean(),
                "pt_clean_mean": pt_clean.mean(),
            },
        }

    ttft_sets = [(name, r["ttft_clean"]) for name, r in results.items()]
    pt_sets = [(name, r["pt_clean"]) for name, r in results.items()]

    plot_hist(ttft_sets, "TTFT (s)", "TTFT Clean Histogram", outdir / "ttft_hist.png")
    plot_violin(ttft_sets, "TTFT (s)", "TTFT Clean Violin", outdir / "ttft_violin.png")
    plot_cdf(ttft_sets, "TTFT (s)", "TTFT Clean CDF", outdir / "ttft_cdf.png")

    plot_hist(pt_sets, "Per-token Latency (s)", "Per-token Clean Histogram", outdir / "pt_hist.png")
    plot_violin(pt_sets, "Per-token Latency (s)", "Per-token Violin", outdir / "pt_violin.png")
    plot_cdf(pt_sets, "Per-token Latency (s)", "Per-token CDF", outdir / "pt_cdf.png")

    generate_report(results, outdir)


if __name__ == "__main__":
    main()
