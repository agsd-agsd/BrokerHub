import matplotlib.pyplot as plt
from matplotlib.ticker import MaxNLocator, PercentFormatter

from draw_diff_hub_common import (
    AXIS_FONTSIZE,
    DEBUG_COLORS,
    HUB_COLORS,
    LEGEND_POSITION,
    LEGEND_SIZE,
    LINE_STYLE1,
    LINE_WIDTH,
    MAIN_OUTPUT_NAME,
    MARKER,
    MARKER_SIZE,
    TICK_FONTSIZE,
    add_subtitles,
    apply_epoch_axis,
    load_all_hubs,
    marker_every,
    output_path,
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


def plot_combined_metrics_with_mer_participation():
    fig, axes = plt.subplots(2, 4, figsize=(32, 12))
    hub_datasets = load_all_hubs()
    hub0, hub1 = hub_datasets
    hub_labels = ("BrokerHub1", "BrokerHub2")
    comparison_epochs = sorted(set(hub0["epoch"]).union(set(hub1["epoch"])))

    subtitles = []

    ax = axes[0, 0]
    line1 = plot_metric(ax, hub0["epoch"], hub0["mer"], f"{hub_labels[0]} MER", HUB_COLORS[0])
    line2 = plot_metric(ax, hub1["epoch"], hub1["mer"], f"{hub_labels[1]} MER", HUB_COLORS[1])
    format_ratio_axis(ax, ylim=(-0.05, 1.05), decimals=0)
    ax.legend([line1, line2], [line1.get_label(), line2.get_label()], loc=LEGEND_POSITION, fontsize=LEGEND_SIZE)
    apply_epoch_axis(ax, comparison_epochs)
    subtitles.append("(a) MER Competition Between Two BrokerHubs")

    ax = axes[0, 1]
    line1 = plot_metric(
        ax,
        hub0["epoch"],
        hub0["participation_rate"],
        f"{hub_labels[0]} Broker Participation",
        HUB_COLORS[0],
    )
    line2 = plot_metric(
        ax,
        hub1["epoch"],
        hub1["participation_rate"],
        f"{hub_labels[1]} Broker Participation",
        HUB_COLORS[1],
    )
    format_ratio_axis(ax, ylim=(-0.05, 1.05), decimals=0)
    ax.legend([line1, line2], [line1.get_label(), line2.get_label()], loc=LEGEND_POSITION, fontsize=LEGEND_SIZE)
    apply_epoch_axis(ax, comparison_epochs)
    subtitles.append("(b) Broker Participation Competition")

    ax = axes[0, 2]
    line1 = plot_metric(
        ax,
        hub0["epoch"],
        hub0["total_ratio"],
        f"{hub_labels[0]} Revenue Ratio",
        HUB_COLORS[0],
    )
    line2 = plot_metric(
        ax,
        hub1["epoch"],
        hub1["total_ratio"],
        f"{hub_labels[1]} Revenue Ratio",
        HUB_COLORS[1],
    )
    format_ratio_axis(ax, decimals=2)
    ax.legend([line1, line2], [line1.get_label(), line2.get_label()], loc=LEGEND_POSITION, fontsize=LEGEND_SIZE)
    apply_epoch_axis(ax, comparison_epochs)
    subtitles.append("(c) Revenue Ratio Competition")

    ax = axes[0, 3]
    line1 = plot_metric(
        ax,
        hub0["epoch"],
        hub0["user_ratio"],
        f"{hub_labels[0]} Broker Revenue Ratio",
        HUB_COLORS[0],
    )
    line2 = plot_metric(
        ax,
        hub1["epoch"],
        hub1["user_ratio"],
        f"{hub_labels[1]} Broker Revenue Ratio",
        HUB_COLORS[1],
    )
    format_ratio_axis(ax, decimals=2)
    ax.legend([line1, line2], [line1.get_label(), line2.get_label()], loc=LEGEND_POSITION, fontsize=LEGEND_SIZE)
    apply_epoch_axis(ax, comparison_epochs)
    subtitles.append("(d) Broker Revenue Ratio Competition")

    for column_index, dataset in enumerate(hub_datasets[:2]):
        ax = axes[1, column_index]
        hub_label = hub_labels[column_index]
        hub_color = HUB_COLORS[column_index]
        line1 = plot_metric(ax, dataset["epoch"], dataset["mer"], f"{hub_label} MER", hub_color)
        format_ratio_axis(ax, ylim=(-0.05, 1.05), decimals=0)

        ax2 = ax.twinx()
        line2 = plot_metric(
            ax2,
            dataset["epoch"],
            dataset["broker_num"],
            "Broker Count",
            DEBUG_COLORS["broker_count"],
            LINE_STYLE1,
        )
        ax2.set_ylabel("Brokers", fontsize=AXIS_FONTSIZE)
        ax2.tick_params(axis="y", labelsize=TICK_FONTSIZE)
        ax2.yaxis.set_major_locator(MaxNLocator(integer=True))
        ax2.set_ylim(-1, max(dataset["broker_num"].max() + 1, 5))
        ax.legend(
            [line1, line2],
            [line1.get_label(), line2.get_label()],
            loc=LEGEND_POSITION,
            fontsize=LEGEND_SIZE,
            handlelength=1.5,
        )
        apply_epoch_axis(ax, dataset["epoch"])
        subtitles.append(f"({chr(101 + column_index)}) {hub_label} MER & Broker Count")

    for column_index, dataset in enumerate(hub_datasets[:2], start=2):
        ax = axes[1, column_index]
        hub_label = hub_labels[column_index - 2]
        line1 = plot_metric(
            ax,
            dataset["epoch"],
            dataset["net_ratio"],
            f"{hub_label} Hub Net Revenue Ratio",
            DEBUG_COLORS["net_ratio"],
        )
        line2 = plot_metric(
            ax,
            dataset["epoch"],
            dataset["user_ratio"],
            f"{hub_label} Broker Revenue Ratio",
            DEBUG_COLORS["broker_ratio"],
            LINE_STYLE1,
        )
        format_ratio_axis(ax, decimals=2)
        ax.legend(
            [line1, line2],
            [line1.get_label(), line2.get_label()],
            loc=LEGEND_POSITION,
            fontsize=LEGEND_SIZE,
            handlelength=1.5,
        )
        apply_epoch_axis(ax, dataset["epoch"])
        subtitles.append(
            f"({chr(101 + column_index)}) {hub_label} Hub Net Revenue vs Broker Revenue"
        )

    add_subtitles(fig, subtitles)
    plt.tight_layout(rect=[0, 0, 1, 0.92], h_pad=4.5, w_pad=3)
    plt.savefig(output_path(MAIN_OUTPUT_NAME), dpi=300, bbox_inches="tight")
    print("plot done.")
    plt.close()


if __name__ == "__main__":
    plot_combined_metrics_with_mer_participation()
