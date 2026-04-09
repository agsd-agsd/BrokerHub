import matplotlib.pyplot as plt
import numpy as np
from matplotlib.ticker import MaxNLocator, PercentFormatter

from draw_diff_hub_common import (
    AXIS_FONTSIZE,
    DEBUG_COLORS,
    EXTENDED_OUTPUT_NAME,
    HUB_COLORS,
    LEGEND_POSITION,
    LEGEND_SIZE,
    LINE_STYLE1,
    LINE_WIDTH,
    MARKER,
    MARKER_SIZE,
    TICK_FONTSIZE,
    add_subtitles,
    apply_epoch_axis,
    load_all_hubs,
    marker_every,
    output_path,
    rank_axis_config,
)


def format_ratio_axis(axis, ylim=None, decimals=0):
    axis.set_ylabel("Ratio (%)", fontsize=AXIS_FONTSIZE)
    axis.yaxis.set_major_formatter(PercentFormatter(xmax=1.0, decimals=decimals))
    axis.tick_params(axis="y", labelsize=TICK_FONTSIZE)
    if ylim is not None:
        axis.set_ylim(*ylim)


def plot_metric(axis, epochs, values, label, color, linestyle="-"):
    return axis.plot(
        epochs,
        values,
        label=label,
        color=color,
        linestyle=linestyle,
        linewidth=LINE_WIDTH,
        marker=MARKER,
        markersize=MARKER_SIZE,
        markevery=marker_every(epochs),
    )[0]


def critical_cap_series(values):
    numeric = np.asarray(values, dtype=float)
    return np.where(numeric > 0, numeric, np.nan)


def plot_shock_markers(axis, dataset, values):
    shock_mask = dataset["shock_flag"] > 0
    if not shock_mask.any():
        return None

    shock_epochs = dataset.loc[shock_mask, "epoch"]
    shock_values = np.asarray(values, dtype=float)[shock_mask.to_numpy()]
    return axis.scatter(
        shock_epochs,
        shock_values,
        label="Shock",
        color=DEBUG_COLORS["shock_marker"],
        marker="x",
        s=55,
        zorder=5,
    )


def plot_extended_metrics():
    fig, axes = plt.subplots(2, 4, figsize=(32, 12))
    hub_datasets = load_all_hubs()
    subtitles = []

    for row_index, dataset in enumerate(hub_datasets):
        hub_label = f"BrokerHub{row_index + 1}"
        hub_color = HUB_COLORS[row_index]
        epochs = dataset["epoch"]

        ax = axes[row_index, 0]
        line1 = plot_metric(ax, epochs, dataset["revenue"], f"{hub_label} Revenue", hub_color)
        ax.set_ylabel("Revenue", fontsize=AXIS_FONTSIZE)
        ax.tick_params(axis="y", labelsize=TICK_FONTSIZE)

        ax2 = ax.twinx()
        line2 = plot_metric(
            ax2,
            epochs,
            dataset["sampled_cross_txs"],
            "Sampled Cross-Txs",
            DEBUG_COLORS["sampled_cross_txs"],
            LINE_STYLE1,
        )
        ax2.set_ylabel("Transactions", fontsize=AXIS_FONTSIZE)
        ax2.tick_params(axis="y", labelsize=TICK_FONTSIZE)
        ax.legend(
            [line1, line2],
            [line1.get_label(), line2.get_label()],
            loc=LEGEND_POSITION,
            fontsize=LEGEND_SIZE,
            handlelength=1.5,
        )
        apply_epoch_axis(ax, epochs)
        subtitles.append(f"({chr(97 + row_index * 4)}) {hub_label} Revenue & Sampled Cross-Txs")

        ax = axes[row_index, 1]
        line1 = plot_metric(
            ax,
            epochs,
            dataset["current_investment"],
            "Current Investment",
            DEBUG_COLORS["current_investment"],
        )
        ax.set_ylabel("Funds", fontsize=AXIS_FONTSIZE)
        ax.tick_params(axis="y", labelsize=TICK_FONTSIZE)
        ax.yaxis.get_offset_text().set_fontsize(TICK_FONTSIZE)

        ax2 = ax.twinx()
        line2 = plot_metric(
            ax2,
            epochs,
            dataset["fund_share"],
            "Fund Share",
            DEBUG_COLORS["fund_share"],
            LINE_STYLE1,
        )
        format_ratio_axis(ax2, ylim=(-0.05, 1.05), decimals=0)
        ax.legend(
            [line1, line2],
            [line1.get_label(), line2.get_label()],
            loc=LEGEND_POSITION,
            fontsize=LEGEND_SIZE,
            handlelength=1.5,
        )
        apply_epoch_axis(ax, epochs)
        subtitles.append(f"({chr(98 + row_index * 4)}) {hub_label} Current Investment & Fund Share")

        ax = axes[row_index, 2]
        line1 = plot_metric(ax, epochs, dataset["mer"], f"{hub_label} MER", hub_color)
        line2 = plot_metric(
            ax,
            epochs,
            critical_cap_series(dataset["critical_mer_cap"]),
            "Critical MER Cap",
            DEBUG_COLORS["critical_mer_cap"],
            LINE_STYLE1,
        )
        shock_markers = plot_shock_markers(ax, dataset, dataset["mer"])
        format_ratio_axis(ax, ylim=(-0.05, 1.05), decimals=0)
        legend_handles = [line1, line2]
        legend_labels = [line1.get_label(), line2.get_label()]
        if shock_markers is not None:
            legend_handles.append(shock_markers)
            legend_labels.append(shock_markers.get_label())
        ax.legend(
            legend_handles,
            legend_labels,
            loc=LEGEND_POSITION,
            fontsize=LEGEND_SIZE,
            handlelength=1.5,
        )
        apply_epoch_axis(ax, epochs)
        subtitles.append(f"({chr(99 + row_index * 4)}) {hub_label} MER, Critical Cap & Shock")

        ax = axes[row_index, 3]
        line1 = plot_metric(
            ax,
            epochs,
            dataset["broker_num"],
            "Broker Count",
            DEBUG_COLORS["broker_count"],
        )
        ax.set_ylabel("Brokers", fontsize=AXIS_FONTSIZE)
        ax.tick_params(axis="y", labelsize=TICK_FONTSIZE)
        ax.yaxis.set_major_locator(MaxNLocator(integer=True))

        ax2 = ax.twinx()
        line2 = plot_metric(
            ax2,
            epochs,
            dataset["dominance_streak"],
            "Dominance Streak",
            DEBUG_COLORS["dominance_streak"],
            LINE_STYLE1,
        )
        line3 = plot_metric(
            ax2,
            epochs,
            dataset["shock_exit_count"],
            "Shock Exit Count",
            DEBUG_COLORS["shock_exit_count"],
            "-.",
        )
        ax2.set_ylabel("Debug Count", fontsize=AXIS_FONTSIZE)
        ax2.tick_params(axis="y", labelsize=TICK_FONTSIZE)
        combined_debug_counts = np.concatenate(
            [
                np.asarray(dataset["dominance_streak"], dtype=float),
                np.asarray(dataset["shock_exit_count"], dtype=float),
            ]
        )
        lower_bound, upper_bound, ticks = rank_axis_config(combined_debug_counts)
        ax2.set_ylim(lower_bound, upper_bound)
        ax2.set_yticks(ticks)
        ax.legend(
            [line1, line2, line3],
            [line1.get_label(), line2.get_label(), line3.get_label()],
            loc=LEGEND_POSITION,
            fontsize=LEGEND_SIZE,
            handlelength=1.5,
        )
        apply_epoch_axis(ax, epochs)
        subtitles.append(f"({chr(100 + row_index * 4)}) {hub_label} Broker Count, Dominance & Shock Exits")

    add_subtitles(fig, subtitles)
    plt.tight_layout(rect=[0, 0, 1, 0.92], h_pad=4.5, w_pad=3)
    plt.savefig(output_path(EXTENDED_OUTPUT_NAME), dpi=300, bbox_inches="tight")
    print("extended plot done.")
    plt.close()


if __name__ == "__main__":
    plot_extended_metrics()
