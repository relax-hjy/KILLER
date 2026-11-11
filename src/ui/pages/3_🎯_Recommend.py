"""
Fund Recommendation Engine — Forward-looking multi-factor scoring + AI strategy picks
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

import pandas as pd
import numpy as np
import streamlit as st
import plotly.express as px
import plotly.graph_objects as go

from src.db.repository import Repository
from config.strategies import (
    STRATEGIES, FORWARD_WEIGHTS, TRADITIONAL_WEIGHTS, FORWARD_RATIO,
    HORIZON_MAP, RETURN_COL_MAP, FACTOR_DEFINITIONS,
)

st.set_page_config(page_title="Fund Recommendations", page_icon="🎯", layout="wide")

st.header("🎯 AI Fund Recommendations")

# ---- Data ----
@st.cache_resource
def get_repo():
    return Repository()

repo = get_repo()

@st.cache_data(ttl=300, show_spinner=False)
def load_funds():
    return repo.get_funds_df()

df_all = load_funds()
if df_all.empty:
    st.warning("No data in database. Go to Screener → Sync Data first.")
    st.stop()

# ====================================================================
# CACHED SCORING ENGINE
# ====================================================================
def _safe_norm(series, invert=False):
    s = pd.to_numeric(series, errors="coerce").dropna()
    if len(s) < 2:
        return pd.Series(0.5, index=series.index)
    if invert:
        return (s.max() - series) / (s.max() - s.min())
    return (series - s.min()) / (s.max() - s.min())

@st.cache_data(ttl=300, show_spinner=False)
def compute_recommendations(_cache_key: str, risk_level: str, horizon: str, min_return: float):
    """All heavy scoring in one cached function."""
    df_all_c = load_funds()
    df = df_all_c.copy()

    horizon_key = HORIZON_MAP.get(horizon, "1Y")
    return_key = RETURN_COL_MAP.get(horizon_key, "return_1y")
    if return_key in df.columns:
        df["_return"] = pd.to_numeric(df[return_key], errors="coerce")
        df = df[df["_return"] >= min_return]

    if df.empty:
        return pd.DataFrame(), pd.DataFrame()

    scores = pd.DataFrame(index=df.index)
    return_vals = pd.to_numeric(df.get("_return", df.get("return_1y", 0)), errors="coerce")
    dd_vals = pd.to_numeric(df.get("max_drawdown", 0), errors="coerce")
    sharpe_vals = pd.to_numeric(df.get("sharpe_ratio", 0), errors="coerce")
    r1m = pd.to_numeric(df.get("return_1m", 0), errors="coerce")
    r3m = pd.to_numeric(df.get("return_3m", 0), errors="coerce")

    scores["return_score"] = _safe_norm(return_vals)
    scores["drawdown_score"] = _safe_norm(dd_vals, invert=True) if dd_vals.notna().sum() > 1 else 0.5
    scores["sharpe_score"] = _safe_norm(sharpe_vals) if sharpe_vals.notna().sum() > 1 else 0.5
    mom_decay = r1m - (r3m / 3)
    scores["momentum_quality"] = _safe_norm(mom_decay)
    extreme_penalty = (return_vals.clip(lower=0) - 15) / 65
    extreme_penalty = extreme_penalty.clip(lower=0)
    scores["not_bubble"] = 1.0 - extreme_penalty
    rr = return_vals / (dd_vals.abs().fillna(10) + 1)
    scores["risk_reward"] = _safe_norm(rr) if dd_vals.notna().sum() > 1 else 0.5

    scores["forward_score"] = sum(FORWARD_WEIGHTS[k] * scores[k] for k in FORWARD_WEIGHTS if k in scores.columns)
    scores["forward_score"] = scores["forward_score"].fillna(0)
    scores["traditional"] = sum(TRADITIONAL_WEIGHTS[k] * scores[k] for k in TRADITIONAL_WEIGHTS if k in scores.columns)
    scores["traditional"] = scores["traditional"].fillna(0)
    scores["final_score"] = (FORWARD_RATIO * scores["forward_score"] + (1-FORWARD_RATIO) * scores["traditional"]).fillna(0).clip(lower=-0.5)

    df_scored = df.copy()
    for c in ["return_score", "drawdown_score", "sharpe_score", "momentum_quality",
               "not_bubble", "risk_reward", "forward_score", "traditional", "final_score"]:
        df_scored[c] = scores[c].values
    df_scored = df_scored.sort_values("final_score", ascending=False).head(50)

    return df_scored, scores

# ---- User Preferences (form batch) ----
st.subheader("⚙️ Investment Profile")

with st.form("recommend_form"):
    col1, col2, col3 = st.columns(3)
    with col1:
        risk_level = st.selectbox("Risk Tolerance", ["Low (Conservative)", "Medium (Balanced)", "High (Aggressive)"])
    with col2:
        horizon = st.selectbox("Investment Horizon", ["Short (<1Y)", "Medium (1-3Y)", "Long (>3Y)"])
    with col3:
        min_return_1y = st.slider("Min 1Y Return (%)", -20.0, 100.0, 0.0, 1.0)
    do_screen = st.form_submit_button("🔍 Generate Recommendations", use_container_width=True, type="primary")

if not do_screen:
    st.info("👆 Set your profile and click 'Generate Recommendations'")
    st.stop()

# ---- Compute (cached) ----
import hashlib
cache_key = hashlib.md5(f"{risk_level}_{horizon}_{min_return_1y}".encode()).hexdigest()
df_scored, scores = compute_recommendations(cache_key, risk_level, horizon, min_return_1y)

if df_scored.empty:
    st.warning("No funds match criteria. Relax filters.")
    st.stop()

# ---- Scoring Model Explanation ----
with st.expander("📐 Scoring Model Explained", expanded=False):
    st.markdown("""
    **How `final_score` is calculated for each fund:**

    | # | Factor | Weight | Meaning |
    |---|--------|--------|---------|
    | ① | Return | 15% ↓ | 1Y return, normalized. Deliberately down-weighted to avoid chasing. |
    | ② | Drawdown | 20% | Max historical drawdown (inverted: smaller = better). |
    | ③ | Sharpe | 15% | Risk-adjusted return. Efficiency of return per unit of volatility. |
    | ④ | Momentum Quality 🆕 | 20% | `return_1m − (return_3m ÷ 3)`. Positive = accelerating trend. Negative = losing steam. |
    | ⑤ | Not Bubble 🆕 | 15% | CRUSH penalty: starts at 15% return, hits 0 at 80%, goes negative above. 450% fund → -5.69. |
    | ⑥ | Risk/Reward 🆕 | 15% | `return_1y ÷ (|max_drawdown| + 1)`. How much return per 1% pain. |

    **Two layers → One score:**
    ```
    forward_score = 0.15×① + 0.20×② + 0.15×③ + 0.20×④ + 0.15×⑤ + 0.15×⑥
    traditional    = 0.40×① + 0.25×② + 0.20×③ + 0.15×④    (old-school ranking)
    final_score    = 0.70 × forward + 0.30 × traditional
    ```
    **Key insight**: Traditional model puts 40% on past returns (chasing). Forward model puts only 15% on returns,
    and 50% on forward-looking signals (④+⑤+⑥). This finds funds with *healthy, sustainable* trends
    rather than those that *already* mooned.
    """)

# ---- Results Table (with all factor scores) ----
st.divider()
st.subheader("🏆 Top Recommendations")

top10 = df_scored.head(10)
display_cols = [
    "code", "name", "fund_type",
    "return_1y", "return_3m", "return_1m",
    "return_score", "drawdown_score", "sharpe_score",
    "momentum_quality", "not_bubble", "risk_reward",
    "forward_score", "traditional", "final_score",
]
available_cols = [c for c in display_cols if c in top10.columns]

top_display = top10[available_cols].copy()
# Format percentages
for c in ["return_1y", "return_3m", "return_1m"]:
    if c in top_display.columns:
        top_display[c] = top_display[c].apply(lambda x: f"{float(x):.1f}%" if pd.notna(x) else "-")
# Format factor scores
for c in ["return_score", "drawdown_score", "sharpe_score", "momentum_quality",
           "not_bubble", "risk_reward", "forward_score", "traditional", "final_score"]:
    if c in top_display.columns:
        top_display[c] = top_display[c].apply(lambda x: f"{float(x):.3f}")

# Rename for readability
col_rename = {
    "return_1y": "1Y Ret", "return_3m": "3M Ret", "return_1m": "1M Ret",
    "return_score": "①Ret", "drawdown_score": "②DD", "sharpe_score": "③Sharpe",
    "momentum_quality": "④MoQ", "not_bubble": "⑤NoBub", "risk_reward": "⑥R/R",
    "forward_score": "FWD", "traditional": "TRAD", "final_score": "FINAL",
}
top_display = top_display.rename(columns={k: v for k, v in col_rename.items() if k in top_display.columns})

st.dataframe(top_display, use_container_width=True, hide_index=True)
st.caption("①-⑥ = factor scores (0-1). MoQ = momentum quality (+accelerating, −fading). NoBub = not-a-bubble score. R/R = risk/reward efficiency. FWD = forward score. TRAD = traditional score.")

# ---- Forward vs Traditional Comparison ----
st.subheader("📊 Forward vs Traditional — Top 10 Comparison")
fig_comp = go.Figure()
top_comp = top10.sort_values("final_score")
fig_comp.add_trace(go.Bar(
    y=top_comp["name"], x=top_comp["forward_score"],
    name="Forward-Looking", orientation="h", marker=dict(color="#1f77b4"),
))
fig_comp.add_trace(go.Bar(
    y=top_comp["name"], x=top_comp["traditional"],
    name="Traditional", orientation="h", marker=dict(color="lightgray", opacity=0.7),
))
fig_comp.update_layout(
    title="Blue = forward-looking score | Gray = traditional (return-heavy)",
    barmode="group", yaxis=dict(autorange="reversed"), xaxis_title="Score",
)
st.plotly_chart(fig_comp, use_container_width=True)

# ---- Factor Breakdown for Top 5 ----
st.subheader("🔬 Factor Breakdown — Top 5")
component_labels = ["①Return", "②Drawdown", "③Sharpe", "④MoQuality", "⑤NotBubble", "⑥Risk/Rewd"]
score_keys = ["return_score", "drawdown_score", "sharpe_score", "momentum_quality", "not_bubble", "risk_reward"]
top5 = df_scored.head(5)

fig_radar = go.Figure()
colors = ["#1f77b4", "#ff7f0e", "#2ca02c", "#d62728", "#9467bd"]
for i, (_, row) in enumerate(top5.iterrows()):
    vals = [float(scores.loc[row.name, k]) for k in score_keys]
    fig_radar.add_trace(go.Scatterpolar(
        r=vals, theta=component_labels,
        name=f"#{i+1} {row['code']}",
        fill="toself", opacity=0.6, line=dict(color=colors[i]),
    ))
fig_radar.update_layout(
    polar=dict(radialaxis=dict(range=[0, 1], tickfont=dict(size=10))),
    title="Radar Chart — higher values toward outer edge = better",
)
st.plotly_chart(fig_radar, use_container_width=True)

# ---- Strategy Explanation ----
with st.expander("🎯 Strategy Rules Reference", expanded=False):
    for name, s in STRATEGIES.items():
        st.markdown(f"**{s['icon']} {name}**: {s['desc']}")
        st.caption(f"Rules: {s['rules']}")
        st.divider()

# ---- Quick Actions ----
st.subheader("🔍 Quick Actions")
top3 = df_scored.head(3)
cols = st.columns(3)
for i, (_, row) in enumerate(top3.iterrows()):
    with cols[i]:
        st.markdown(f"**#{i+1}  {row['code']}**")
        st.caption(str(row['name'])[:40])
        c1, c2 = st.columns(2)
        with c1:
            if st.button(f"📈 Detail", key=f"det_{row['code']}"):
                st.session_state["detail_fund_code"] = str(row["code"])
                st.session_state["detail_fund_name"] = str(row["name"])
                st.switch_page("pages/1_📈_Fund_Detail.py")
        with c2:
            if st.button(f"💰 Strategy", key=f"str_{row['code']}"):
                st.session_state["detail_fund_code"] = str(row["code"])
                st.session_state["detail_fund_name"] = str(row["name"])
                st.switch_page("pages/2_💰_Strategy.py")

# ---- AI Strategy Picks ----
st.divider()
st.subheader("🤖 AI Strategy Picks")

col_s1, col_s2 = st.columns([1, 2])
with col_s1:
    ai_strategy = st.selectbox(
        "Investment Strategy",
        list(STRATEGIES.keys()),
        format_func=lambda x: f"{STRATEGIES[x]['icon']} {x}",
    )
with col_s2:
    st.caption(STRATEGIES[ai_strategy]["desc"])

if st.button(f"🤖 Let AI Pick ({ai_strategy})", use_container_width=True, type="primary"):
    import os
    from openai import OpenAI

    top15 = df_scored.head(15)
    candidates_text = "Candidates:\n"
    for i, (_, r) in enumerate(top15.iterrows()):
        parts = [f"#{i+1} {r['code']} {r['name']} Type={r['fund_type']}"]
        for k in ["return_1m", "return_3m", "return_6m", "return_1y"]:
            if k in r and pd.notna(r[k]):
                parts.append(f"{k}={r[k]:.2f}%")
        for k in ["momentum_quality", "risk_reward", "not_bubble", "final_score"]:
            if k in r and pd.notna(r[k]):
                parts.append(f"{k}={r[k]:.3f}")
        candidates_text += " | ".join(parts) + "\n"

    strat = STRATEGIES[ai_strategy]
    prompt = f"""Apply the **{ai_strategy}** strategy to pick 3 best funds.

Strategy: {strat['desc']}
Rules: {strat['rules']}

Investor: {risk_level}, horizon={horizon}, all fund types considered

{candidates_text}

Task:
1. Pick TOP 3 using the strategy rules. For each: WHY it fits, Strategy Fit 1-10, % allocation
2. Key risk for each pick
3. When to SELL each position
4. Compared to traditional return-only ranking — what's DIFFERENT about your picks?

Be concise. Reply in EN + ZH."""

    api_key = os.environ.get("DEEPSEEK_API_KEY")
    if api_key:
        with st.spinner("AI analyzing..."):
            try:
                client = OpenAI(api_key=api_key, base_url="https://api.deepseek.com")
                resp = client.chat.completions.create(
                    model="deepseek-chat",
                    messages=[
                        {"role": "system", "content": "You are a forward-looking fund advisor. You prioritize momentum quality, valuation risk, and risk/reward over raw past returns."},
                        {"role": "user", "content": prompt},
                    ],
                    max_tokens=2500,
                )
                st.markdown(resp.choices[0].message.content)
            except Exception as e:
                st.error(f"Failed: {e}")
    else:
        st.warning("👈 Set DeepSeek API Key in the sidebar")
        st.info("Top 3 by forward-looking score:")
        for i, (_, r) in enumerate(top3.iterrows()):
            st.markdown(
                f"**#{i+1} {r['code']} — {r['name']}**  "
                f"Score={r['final_score']:.3f} | "
                f"1Y={r.get('return_1y', 'N/A')} | "
                f"MoQuality={r.get('momentum_quality', 'N/A'):.3f}"
            )

st.divider()
fw_pct = int(FORWARD_RATIO * 100)
tr_pct = 100 - fw_pct
fw_desc = " + ".join([FACTOR_DEFINITIONS[k]['label'] for k in FORWARD_WEIGHTS])
tr_desc = " + ".join([FACTOR_DEFINITIONS[k]['label'] for k in TRADITIONAL_WEIGHTS if TRADITIONAL_WEIGHTS[k] > 0])
st.caption(
    f"Scoring: {fw_pct}% forward-looking ({fw_desc}) + {tr_pct}% traditional ({tr_desc}). "
    "Edit config/strategies.py to tune weights → Rerun → live."
)
