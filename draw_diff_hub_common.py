import math
import os
import re

import numpy as np
import pandas as pd


FIGSIZE = (8, 6)
LEGEND_POSITION = "upper right"
STEP = 50
RATIO_STEP = 0.2
AXIS_FONTSIZE = 25
TICK_FONTSIZE = 25
SCIENTIFIC_FONTSIZE = 20
SUPTITLE_FONTSIZE = 24
SUBTITLE_FONTSIZE = 24
LEGEND_SIZE = 20
LINE_WIDTH = 2
SUBLINE_WIDTH = 1
LINE_STYLE1 = "--"
LINE_STYLE2 = "-"
MARKER = "o"
MARKER_SIZE = 3

COLOR_CYCLE = [
    "#1f77b4",
    "#ff7f0e",
    "#2ca02c",
    "#d62728",
    "#9467bd",
    "#8c564b",
    "#e377c2",
    "#7f7f7f",
    "#bcbd22",
    "#17becf",
]

HUB_COLORS = (COLOR_CYCLE[0], COLOR_CYCLE[1])
DEBUG_COLORS = {
    "broker_count": COLOR_CYCLE[4],
    "net_ratio": "#00CED1",
    "broker_ratio": "#BA55D3",
    "sampled_cross_txs": COLOR_CYCLE[5],
    "current_investment": COLOR_CYCLE[8],
    "predicted_investment": COLOR_CYCLE[9],
    "rank": COLOR_CYCLE[2],
    "fund_share": COLOR_CYCLE[2],
    "critical_mer_cap": COLOR_CYCLE[3],
    "dominance_streak": COLOR_CYCLE[7],
    "shock_exit_count": COLOR_CYCLE[6],
    "shock_marker": COLOR_CYCLE[3],
}

DEFAULT_BROKER_NUM = 20
HUB_CSV_PATHS = ("./hubres/hub0.csv", "./hubres/hub1.csv")
MAIN_OUTPUT_NAME = "AsterYao_brokerhub_combined_metrics.pdf"
EXTENDED_OUTPUT_NAME = "AsterYao_brokerhub_extended_metrics.pdf"

SUBTITLE_POSITIONS = [
    (0.127, 0.465),
    (0.39, 0.465),
    (0.64, 0.465),
    (0.91, 0.465),
    (0.127, -0.01),
    (0.39, -0.01),
    (0.64, -0.01),
    (0.91, -0.01),
]


def _constant_series(index, value):
    return pd.Series(np.full(len(index), value, dtype=float), index=index)


def _fallback_series(index, fallback):
    if isinstance(fallback, pd.Series):
        return pd.to_numeric(fallback, errors="coerce").reindex(index).fillna(0.0)
    return _constant_series(index, fallback)


def _numeric_column(dataframe, aliases, fallback=0.0):
    for alias in aliases:
        if alias in dataframe.columns:
            series = pd.to_numeric(dataframe[alias], errors="coerce")
            return series.where(series.notna(), _fallback_series(dataframe.index, fallback)).astype(float)
    return _fallback_series(dataframe.index, fallback).astype(float)


def _epoch_column(dataframe):
    fallback = pd.Series(np.arange(1, len(dataframe) + 1), index=dataframe.index, dtype=float)
    epoch = _numeric_column(dataframe, ("epoch", "Epoch"), fallback=fallback)
    return epoch.round().astype(int)


def safe_divide_series(numerator, denominator):
    numerator = pd.to_numeric(numerator, errors="coerce").astype(float)
    denominator = pd.to_numeric(denominator, errors="coerce").astype(float)
    result = pd.Series(np.zeros(len(numerator), dtype=float), index=numerator.index)
    valid = denominator.notna() & (denominator != 0)
    result.loc[valid] = numerator.loc[valid] / denominator.loc[valid]
    return result


def read_csv_blocks(path):
    with open(path, "r", encoding="utf-8", errors="ignore") as file:
        content = file.read()

    normalized_content = re.sub(r"(?<![\r\n])(epoch,)", r"\n\1", content)
    rows = []
    current_header = None

    for raw_line in normalized_content.splitlines():
        line = raw_line.strip()
        if not line:
            continue

        if line.lower().startswith("epoch,"):
            current_header = [item.strip() for item in line.split(",")]
            continue

        if current_header is None:
            continue

        values = [item.strip() for item in line.split(",")]
        if len(values) != len(current_header):
            continue
        rows.append(dict(zip(current_header, values)))

    return pd.DataFrame(rows)


def load_hub_dataframe(path, default_broker_count=DEFAULT_BROKER_NUM):
    dataframe = read_csv_blocks(path)
    normalized = pd.DataFrame(index=dataframe.index)

    normalized["epoch"] = _epoch_column(dataframe)
    normalized["revenue"] = _numeric_column(dataframe, ("revenue",))
    normalized["broker_num"] = _numeric_column(dataframe, ("broker_num",)).round().astype(int)
    normalized["mer"] = _numeric_column(dataframe, ("mer", "MER"))
    normalized["fund"] = _numeric_column(dataframe, ("fund",))
    normalized["rank"] = _numeric_column(dataframe, ("Rank", "rank")).round().astype(int)

    if default_broker_count > 0:
        default_participation = normalized["broker_num"].astype(float) / float(default_broker_count)
    else:
        default_participation = _constant_series(dataframe.index, 0.0)
    normalized["participation_rate"] = _numeric_column(
        dataframe,
        ("participation_rate",),
        fallback=default_participation,
    )

    normalized["current_investment"] = _numeric_column(
        dataframe,
        ("current_investment",),
        fallback=normalized["fund"],
    )
    normalized["sampled_cross_txs"] = _numeric_column(
        dataframe,
        ("sampled_cross_txs",),
        fallback=0.0,
    )
    normalized["predicted_investment"] = _numeric_column(
        dataframe,
        ("predicted_investment",),
        fallback=normalized["current_investment"],
    )
    normalized["fund_share"] = _numeric_column(
        dataframe,
        ("fund_share",),
        fallback=0.0,
    )
    normalized["dominance_streak"] = _numeric_column(
        dataframe,
        ("dominance_streak",),
        fallback=0.0,
    )
    normalized["critical_mer_cap"] = _numeric_column(
        dataframe,
        ("critical_mer_cap",),
        fallback=0.0,
    )
    normalized["shock_exit_count"] = _numeric_column(
        dataframe,
        ("shock_exit_count",),
        fallback=0.0,
    )
    normalized["shock_fund_drop"] = _numeric_column(
        dataframe,
        ("shock_fund_drop",),
        fallback=0.0,
    )
    if "optimizer_phase" in dataframe.columns:
        normalized["optimizer_phase"] = dataframe["optimizer_phase"].fillna("").astype(str)
    else:
        normalized["optimizer_phase"] = ""
    normalized["shock_flag"] = (
        normalized["optimizer_phase"].eq("shock") | (normalized["shock_exit_count"] > 0)
    ).astype(float)

    normalized["total_ratio"] = safe_divide_series(normalized["revenue"], normalized["fund"])
    normalized["net_ratio"] = normalized["total_ratio"] * normalized["mer"]
    normalized["user_ratio"] = normalized["total_ratio"] * (1.0 - normalized["mer"])

    return normalized


def load_all_hubs(paths=HUB_CSV_PATHS):
    return [load_hub_dataframe(path) for path in paths]


def marker_every(epochs):
    point_count = len(list(epochs))
    if point_count <= 25:
        return 1
    if point_count <= 60:
        return 3
    if point_count <= 100:
        return 5
    return max(1, point_count // 20)


def resolve_epoch_ticks(epochs):
    if len(epochs) == 0:
        return [1]

    max_epoch = int(max(epochs))
    if max_epoch <= 10:
        step = 1
    elif max_epoch <= 50:
        step = 10
    elif max_epoch <= 100:
        step = 20
    elif max_epoch <= 300:
        step = STEP
    else:
        step = int(math.ceil(max_epoch / 6.0 / 10.0) * 10)

    ticks = list(range(step, max_epoch + 1, step))
    if 1 not in ticks:
        ticks = [1] + ticks
    if ticks[-1] != max_epoch:
        ticks.append(max_epoch)
    return ticks


def apply_epoch_axis(axis, epochs):
    epoch_values = list(epochs)
    if not epoch_values:
        return

    ticks = resolve_epoch_ticks(epoch_values)
    left = float(min(epoch_values))
    right = float(max(epoch_values))
    if left == right:
        left -= 0.5
        right += 0.5
    axis.set_xlim(left, right)
    axis.set_xticks(ticks)
    axis.tick_params(axis="x", labelsize=TICK_FONTSIZE)
    axis.set_xlabel("Epoch", fontsize=AXIS_FONTSIZE)


def rank_axis_config(ranks):
    rank_values = [int(value) for value in pd.to_numeric(pd.Series(ranks), errors="coerce").dropna().tolist()]
    if not rank_values:
        return 0, 1, [0, 1]

    rank_min = min(rank_values)
    rank_max = max(rank_values)
    padding = max(1, int(math.ceil((rank_max - rank_min + 1) * 0.2)))
    lower_bound = max(0, rank_min - padding)
    upper_bound = rank_max + padding
    span = upper_bound - lower_bound

    if span <= 8:
        step = 1
    elif span <= 20:
        step = 2
    else:
        step = 5

    ticks = list(range(lower_bound, upper_bound + 1, step))
    if ticks[-1] != upper_bound:
        ticks.append(upper_bound)
    return lower_bound, upper_bound, ticks


def add_subtitles(figure, subtitles):
    for subtitle, position in zip(subtitles, SUBTITLE_POSITIONS):
        figure.text(position[0], position[1], subtitle, ha="center", fontsize=SUBTITLE_FONTSIZE)


def output_path(filename):
    os.makedirs("./hubres", exist_ok=True)
    return os.path.join("./hubres", filename)
