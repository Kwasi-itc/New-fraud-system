import pandas as pd
import matplotlib.pyplot as plt
import numpy as np
import re
from pathlib import Path

OUTPUT_DIR = Path(__file__).resolve().parent / "graphs"
OUTPUT_DIR.mkdir(exist_ok=True)

# Data parsing
data = [
    ["Wallet Transfer Fraud Screening", 76.83, 65.84, 117.29, 142.94, 61.66, 149.35],
    ["Account Takeover Detection", 56.54, 55.70, 63.04, 65.29, 50.40, 65.85],
    ["Merchant Abuse Monitoring", 66.80, 65.02, 77.62, 79.25, 61.07, 79.66],
    ["High Value Transaction Review", 58.44, 54.62, 77.27, 83.07, 50.73, 84.52],
    ["Card Payment Authorization Risk", 49.79, 53.72, 57.08, 57.52, 10.95, 57.63],
    ["Bank Transfer Risk Assessment", 64.91, 63.22, 72.40, 73.37, 58.55, 73.61],
    ["Cash-Out Fraud Monitoring", 62.34, 61.83, 66.69, 68.92, 59.68, 69.48],
    ["New Beneficiary Payment Review", 60.32, 55.52, 83.38, 94.03, 53.25, 96.69],
    ["Dormant Account Reactivation Risk", 53.65, 58.00, 61.80, 62.11, 10.61, 62.19],
    ["Cross-Border or Proxy Access Review", 59.32, 60.86, 67.24, 67.74, 46.15, 67.86],
    ["Chango Group Contribution Fraud Monitoring", 65.51, 63.53, 74.05, 76.91, 61.34, 77.62],
    ["Chango Disbursement and Borrowing Risk Review", 9.48, 9.06, 11.21, 11.39, 7.98, 11.44],
    ["Regulatory Reporting Review", 11.96, 12.75, 12.98, 13.03, 10.00, 13.04],
    ["Overall", 55.53, 59.89, 79.05, 95.23, 7.98, 149.35]
]

df = pd.DataFrame(data, columns=["Scenario", "Avg", "p50", "p95", "p99", "Min", "Max"])

metrics = ["Min", "Avg", "p99", "Max"]
colors = ["#a8a29e", "#3b82f6", "#1e3a8a", "#ef4444"]


def scenario_filename(scenario_name):
    slug = re.sub(r"[^a-z0-9]+", "-", scenario_name.lower()).strip("-")
    return f"{slug}.png"


def plot_scenario(ax, row):
    scenario_name = row["Scenario"]
    values = [row[m] for m in metrics]

    bars = ax.bar(metrics, values, color=colors, edgecolor='black', alpha=0.85)
    ax.set_title(scenario_name, fontsize=10, fontweight='bold', pad=8)
    ax.set_ylabel("Latency (ms)", fontsize=9)
    ax.grid(axis='y', linestyle='--', alpha=0.5)

    for bar in bars:
        yval = bar.get_height()
        ax.text(
            bar.get_x() + bar.get_width() / 2.0,
            yval + (max(values) * 0.02),
            f"{yval:.1f}",
            ha='center',
            va='bottom',
            fontsize=8,
        )

    ax.set_ylim(0, max(values) * 1.15)


for _, row in df.iterrows():
    fig, ax = plt.subplots(figsize=(8, 5))
    plot_scenario(ax, row)
    fig.tight_layout()
    fig.savefig(OUTPUT_DIR / scenario_filename(row["Scenario"]), dpi=300)
    plt.close(fig)


# Set up a grid of subplots (14 scenarios -> 4 rows, 4 columns)
fig, axes = plt.subplots(4, 4, figsize=(20, 16), sharey=False)
axes = axes.flatten()

for i, row in df.iterrows():
    ax = axes[i]
    plot_scenario(ax, row)

# Hide any unused subplots
for j in range(len(df), len(axes)):
    fig.delaxes(axes[j])

plt.suptitle("Latency Metrics Profile per Scenario (Excluding p50 & p95)", fontsize=18, fontweight='bold', y=0.98)
plt.tight_layout()
fig.savefig(OUTPUT_DIR / "scenario_individual_plots.png", dpi=300)
plt.close(fig)

print(f"Created {len(df)} individual graphs and 1 grid graph in {OUTPUT_DIR}")
