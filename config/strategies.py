"""
Investment Strategy Definitions & Scoring Model

All strategies and factor weights are defined here for easy tuning.
Pages import from this module instead of hardcoding.
"""

# ============================================================================
# 1. AI Investment Strategies
# ============================================================================

STRATEGIES: dict[str, dict] = {
    "Momentum": {
        "icon": "🚀",
        "desc": "Buy winners with accelerating trend. Strong recent returns, above MA50.",
        "rules": (
            "Pick funds with HIGH momentum_quality (accelerating). "
            "Prefer strong return_3m and return_6m. "
            "Avoid decelerating funds (1M < 3M average). "
            "Diversify across 2-3 types."
        ),
    },
    "Deep Value": {
        "icon": "💎",
        "desc": "Buy beaten-down quality. Low historical percentile, undervalued.",
        "rules": (
            "Pick funds with LOW return_1y that still have positive Sharpe. "
            "High risk_reward ratio preferred. "
            "Look for turnaround: positive 1M return despite weak 1Y. "
            "Avoid bubble stocks."
        ),
    },
    "Quality GARP": {
        "icon": "⭐",
        "desc": "Growth at Reasonable Price. Good returns + controlled risk + fair valuation.",
        "rules": (
            "Balance return_score AND not_bubble. "
            "High Sharpe + momentum_quality. "
            "Avoid extreme returns (>50% 1Y). "
            "Prefer funds with strong risk_reward."
        ),
    },
    "Defensive": {
        "icon": "🛡️",
        "desc": "Capital preservation. Low drawdown, high Sharpe, stable across periods.",
        "rules": (
            "Highest drawdown_score + sharpe_score. "
            "Consistent positive returns across ALL periods. "
            "Low not_bubble risk. "
            "Ignore momentum — focus on stability."
        ),
    },
    "Forward-Looking": {
        "icon": "🔮",
        "desc": "Smart model. Momentum quality + bubble risk + risk/reward to find tomorrow's winners.",
        "rules": (
            "Trust the forward_score ranking. "
            "Prioritize high momentum_quality AND high not_bubble. "
            "This means: accelerating trend WITHOUT being overheated. "
            "High risk_reward = efficient returns."
        ),
    },
    "Trend Following": {
        "icon": "📈",
        "desc": "Follow the trend. Above MA50/MA200, strong recent momentum.",
        "rules": (
            "Only funds with positive 3M and 6M returns. "
            "Current NAV above MA50 preferred. "
            "Rising trend over past 6 months. "
            "Cut losers quickly — avoid anything with negative 3M return."
        ),
    },
}

# ============================================================================
# 2. Factor Definitions
# ============================================================================
# Each factor produces a 0-1 score. Explained inline.

FACTOR_DEFINITIONS = {
    "return_score": {
        "label": "Return",
        "desc": "Historical return (1Y or horizon-matched). Higher is better.",
        "source": "DB column: return_1y / return_6m / return_3y",
    },
    "drawdown_score": {
        "label": "Drawdown",
        "desc": "Max drawdown control. Lower drawdown = higher score (inverted).",
        "source": "DB column: max_drawdown (from ranking API)",
    },
    "sharpe_score": {
        "label": "Sharpe",
        "desc": "Risk-adjusted return. Higher Sharpe = better efficiency.",
        "source": "DB column: sharpe_ratio (from ranking API)",
    },
    "momentum_quality": {
        "label": "Momentum Quality",
        "desc": "Is the trend accelerating? return_1m - (return_3m / 3). "
               "Positive = recent month beats 3-month average (accelerating). "
               "Negative = losing steam (decelerating).",
        "source": "Calculated: return_1m - (return_3m / 3)",
    },
    "not_bubble": {
        "label": "Not Bubble",
        "desc": "CRUSHES extreme returns. Penalty: (return - 15%) / 65. "
               "Score = 1 - penalty (unbounded negative for >80% returns). "
               "450% fund → score = -5.69, effectively dead last. "
               "Prevents buying into overheated funds due for mean reversion.",
        "source": "Calculated: 1 - max(0, (return_1y - 15) / 65)",
    },
    "risk_reward": {
        "label": "Risk/Reward",
        "desc": "Return per unit of max pain. return_1y / (abs(max_drawdown) + 1). "
               "Higher = more efficient return generation.",
        "source": "Calculated: return_1y / (abs(max_drawdown) + 1)",
    },
}

# ============================================================================
# 3. Scoring Model Weights
# ============================================================================

# Forward-looking weights (used for final ranking)
FORWARD_WEIGHTS = {
    "return_score":       0.15,  # DOWN-WEIGHTED to avoid chasing past performance
    "drawdown_score":     0.20,
    "sharpe_score":       0.15,
    "momentum_quality":   0.20,  # Trend acceleration — key forward-looking signal
    "not_bubble":         0.15,  # Avoid overheated funds
    "risk_reward":        0.15,  # Efficiency of return generation
}

# Traditional weights (for comparison / legacy)
TRADITIONAL_WEIGHTS = {
    "return_score":       0.40,
    "drawdown_score":     0.25,
    "sharpe_score":       0.20,
    "momentum_quality":   0.15,
    "not_bubble":         0.00,
    "risk_reward":        0.00,
}

# Blend ratio: final_score = FORWARD_RATIO * forward + (1 - FORWARD_RATIO) * traditional
FORWARD_RATIO = 0.70  # 70% forward-looking, 30% traditional

# ============================================================================
# 4. Fund Type Mapping (English UI → Chinese DB pattern)
# ============================================================================

TYPE_PATTERN_MAP = {
    "Hybrid": "混合",
    "Equity": "股票",
    "Bond": "债券",
    "Money Market": "货币",
    "QDII": "QDII",
    "FOF": "FOF",
    "Index": "指数",
}

# ============================================================================
# 5. Period Mappings
# ============================================================================

HORIZON_MAP = {
    "Short (<1Y)": "6M",
    "Medium (1-3Y)": "1Y",
    "Long (>3Y)": "3Y",
}

RETURN_COL_MAP = {
    "6M": "return_6m",
    "1Y": "return_1y",
    "3Y": "return_3y",
}

PERIODS_FOR_DETAIL = [
    ("1M", 21),
    ("3M", 63),
    ("6M", 126),
    ("1Y", 252),
    ("2Y", 504),
    ("3Y", 756),
    ("Since Inception", None),
]
