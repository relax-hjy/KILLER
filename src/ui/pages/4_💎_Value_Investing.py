"""
Value Investing — Find undervalued funds by industry & valuation
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
from config.strategies import TYPE_PATTERN_MAP

st.set_page_config(page_title="Value Investing", page_icon="💎", layout="wide")
st.header("💎 Value Investing — Buy Quality at a Discount")

# ---- Data (cached) ----
@st.cache_resource
def get_repo():
    return Repository()

repo = get_repo()

@st.cache_data(ttl=300, show_spinner=False)
def load_funds():
    return repo.get_funds_df()

# ---- Helpers (cached) ----
def safe_norm(series, invert=False):
    s = pd.to_numeric(series, errors="coerce").dropna()
    if len(s) < 2:
        return pd.Series(0.5, index=series.index)
    if invert:
        return (s.max() - series) / (s.max() - s.min())
    return (series - s.min()) / (s.max() - s.min())

@st.cache_data(ttl=300, show_spinner=False)
def compute_value_scores(_df_key: str, max_1y: float, min_sharpe: float, max_dd: float, recent: str):
    """Cache heavy scoring computation. _df_key is a hash for cache busting."""
    df_all = load_funds()
    df = df_all.copy()

    r1y = pd.to_numeric(df.get("return_1y", 0), errors="coerce")
    r1m = pd.to_numeric(df.get("return_1m", 0), errors="coerce")
    r3m = pd.to_numeric(df.get("return_3m", 0), errors="coerce")
    sharpe_v = pd.to_numeric(df.get("sharpe_ratio", 0), errors="coerce")
    dd_v = pd.to_numeric(df.get("max_drawdown", 0), errors="coerce")

    df = df[(r1y <= max_1y) | (r1y.isna())]
    df = df[(sharpe_v >= min_sharpe) | (sharpe_v.isna())]
    df = df[(dd_v.abs() <= max_dd) | (dd_v.isna())]
    if recent == "Down 1M":
        df = df[(r1m < 0) | (r1m.isna())]
    elif recent == "Down 3M":
        df = df[(r3m < 0) | (r3m.isna())]

    if df.empty:
        return pd.DataFrame()

    scores = pd.DataFrame(index=df.index)
    scores["margin_safety"] = safe_norm(r1y.loc[df.index], invert=True)
    scores["quality"] = 0.5 * safe_norm(sharpe_v.loc[df.index]).fillna(0.5) + 0.5 * safe_norm(dd_v.loc[df.index], invert=True).fillna(0.5)
    turnaround = pd.to_numeric(df.get("return_1m", 0), errors="coerce") - pd.to_numeric(df.get("return_3m", 0), errors="coerce") / 3
    scores["turnaround"] = safe_norm(turnaround)
    broken = (r1y < -40).astype(float) + (r3m < -30).astype(float)
    scores["not_broken"] = 1.0 - broken.clip(0, 1)
    scores["value_score"] = (0.40*scores["margin_safety"] + 0.25*scores["quality"] + 0.20*scores["turnaround"] + 0.15*scores["not_broken"]).fillna(0)

    # Industry growth
    df_all_g = df_all.copy()
    df_all_g["ft_top"] = df_all_g["fund_type"].fillna("Unknown").str.extract(r"^([^\-]+)")[0].fillna("Other")
    ind_growth = df_all_g.groupby("ft_top").agg(
        med_3y=("return_3y", lambda x: pd.to_numeric(x, errors="coerce").median()),
        med_1y=("return_1y", lambda x: pd.to_numeric(x, errors="coerce").median()),
        med_6m=("return_6m", lambda x: pd.to_numeric(x, errors="coerce").median()),
        fund_count=("code", "count"),
    ).fillna(0)
    ind_growth["growth_rate"] = 0.50*ind_growth["med_1y"].clip(-50,100) + 0.30*ind_growth["med_6m"].clip(-50,100) + 0.20*ind_growth["med_3y"].clip(-50,100)
    gv = ind_growth["growth_rate"]
    growth_norm = (gv - gv.min()) / (gv.max() - gv.min() + 0.01)
    growth_adj = (growth_norm - 0.5).clip(-0.3, 0.3)

    df_scored = df.copy()
    for c in ["margin_safety", "quality", "turnaround", "not_broken", "value_score"]:
        df_scored[c] = scores[c].values
    df_scored["ft_top"] = df_scored["fund_type"].fillna("Unknown").str.extract(r"^([^\-]+)")[0].fillna("Other")
    df_scored["growth_adj"] = df_scored["ft_top"].map(growth_adj).fillna(0)
    df_scored["growth_rate"] = df_scored["ft_top"].map(ind_growth["growth_rate"]).fillna(0)
    df_scored["value_adj"] = (df_scored["value_score"] + df_scored["growth_adj"]).clip(0, 1)

    return df_scored.sort_values("value_adj", ascending=False).head(50)

# ---- UI Filters (use form to batch) ----
with st.form("value_filters"):
    col1, col2, col3 = st.columns(3)
    with col1:
        max_1y_return = st.slider("Max 1Y Return (%)", 0.0, 100.0, 30.0, 1.0)
        recent_drop = st.selectbox("Recent Trend", ["Any", "Down 1M", "Down 3M"])
    with col2:
        min_sharpe = st.slider("Min Sharpe Ratio", -2.0, 3.0, 0.0, 0.1)
    with col3:
        max_drawdown_filter = st.slider("Max Drawdown (%)", 0.0, 60.0, 25.0, 1.0)
    submitted = st.form_submit_button("🔍 Find Value", use_container_width=True, type="primary")

if not submitted:
    st.info("👆 Set filters and click 'Find Value' to search for undervalued funds")
    st.stop()

# ---- Compute (cached) ----
with st.spinner("Scanning for value opportunities..."):
    import hashlib
    cache_key = hashlib.md5(f"{max_1y_return}_{min_sharpe}_{max_drawdown_filter}_{recent_drop}".encode()).hexdigest()
    df_scored = compute_value_scores(cache_key, max_1y_return, min_sharpe, max_drawdown_filter, recent_drop)

if df_scored.empty:
    st.warning("No value candidates. Relax filters.")
    st.stop()

# ---- Results Table ----
st.subheader(f"💎 Value Opportunities — {len(df_scored)} candidates")

top20 = df_scored.head(20)
display_cols = [
    "code", "name", "fund_type",
    "return_1y", "return_3m", "return_1m",
    "sharpe_ratio", "max_drawdown",
    "growth_rate", "value_score", "value_adj",
]
available_cols = [c for c in display_cols if c in top20.columns]
top_display = top20[available_cols].copy()
for c in ["return_1y", "return_3m", "return_1m", "max_drawdown"]:
    if c in top_display.columns:
        top_display[c] = top_display[c].apply(lambda x: f"{float(x):.1f}%" if pd.notna(x) else "-")
for c in ["growth_rate"]:
    if c in top_display.columns:
        top_display[c] = top_display[c].apply(lambda x: f"{float(x):.0f}%" if pd.notna(x) else "-")
for c in ["value_score", "value_adj"]:
    if c in top_display.columns:
        top_display[c] = top_display[c].apply(lambda x: f"{float(x):.3f}")

col_rename = {
    "return_1y": "1Y Ret", "return_3m": "3M Ret", "return_1m": "1M Ret",
    "sharpe_ratio": "Sharpe", "max_drawdown": "MaxDD",
    "growth_rate": "Ind.Grw", "value_score": "VALUE", "value_adj": "VALUE+",
}
top_display = top_display.rename(columns={k: v for k, v in col_rename.items() if k in top_display.columns})
st.dataframe(top_display, use_container_width=True, hide_index=True)

# ---- Charts ----
col_c1, col_c2 = st.columns(2)
with col_c1:
    fig = px.bar(
        top20.sort_values("value_adj").tail(15),
        x="value_adj", y="name", orientation="h",
        title="Value Picks — Growth-Adjusted",
        color="value_adj", color_continuous_scale="RdYlGn",
        labels={"value_adj": "VALUE+", "name": ""},
    )
    fig.update_layout(yaxis=dict(autorange="reversed"))
    st.plotly_chart(fig, use_container_width=True)

with col_c2:
    # Factor breakdown
    top5 = df_scored.head(5)
    components = ["Safety", "Quality", "Turn", "Not Broken"]
    keys = ["margin_safety", "quality", "turnaround", "not_broken"]
    fig2 = go.Figure()
    for _, row in top5.iterrows():
        vals = [float(row.get(k, 0.5)) for k in keys]
        fig2.add_trace(go.Bar(name=f"{row['code']} {str(row['name'])[:12]}", x=components, y=vals))
    fig2.update_layout(barmode="group", yaxis_title="Score", title="Top 5 Factor Breakdown")
    st.plotly_chart(fig2, use_container_width=True)

# ---- Industry Analysis (on demand) ----
st.divider()
st.subheader("🏭 Industry Analysis")
if st.button("📊 Analyze Industries (slow — makes API calls)", use_container_width=True):
    import akshare as ak
    with st.spinner("Fetching industry allocations for top 5 value picks..."):
        industry_data = []
        for _, row in df_scored.head(5).iterrows():
            try:
                df_ind = ak.fund_portfolio_industry_allocation_em(symbol=str(row["code"]), date="2026")
                if not df_ind.empty:
                    for _, ir in df_ind.iterrows():
                        industry_data.append({
                            "fund": f"{row['code']} {str(row['name'])[:15]}",
                            "industry": ir.get("行业名称", "Unknown"),
                            "weight": float(ir.get("占净值比例", 0)),
                            "value_adj": float(row["value_adj"]),
                        })
            except Exception:
                pass
        if industry_data:
            df_ind_all = pd.DataFrame(industry_data)
            col_i1, col_i2 = st.columns(2)
            with col_i1:
                fig_i = px.treemap(df_ind_all, path=["industry"], values="weight",
                                   color="weight", color_continuous_scale="Blues",
                                   title="Where Value Funds Invest")
                st.plotly_chart(fig_i, use_container_width=True)
            with col_i2:
                ind_s = df_ind_all.groupby("industry").agg(
                    total_w=("weight", "sum"), n_funds=("fund", "nunique"),
                ).sort_values("total_w", ascending=False).head(10)
                st.dataframe(ind_s, use_container_width=True)

# ---- AI Value Analysis ----
st.divider()
if st.button("🤖 AI Value Analysis", use_container_width=True):
    import os
    from openai import OpenAI

    top5 = df_scored.head(5)
    cand = "\n".join(
        f"#{i+1} {r['code']} {r['name']} 1Y={r.get('return_1y','N/A')} "
        f"Sharpe={r.get('sharpe_ratio','N/A')} VALUE+={r['value_adj']:.3f} Grw={r.get('growth_rate','N/A')}"
        for i, (_, r) in enumerate(top5.iterrows())
    )
    prompt = f"""Value investor analysis. Find the best bargain among:
{cand}
1. Best value pick + why
2. Is this a value trap or real opportunity?
3. Position sizing advice
Reply EN+ZH."""

    api_key = os.environ.get("DEEPSEEK_API_KEY")
    if api_key:
        with st.spinner("AI analyzing..."):
            try:
                client = OpenAI(api_key=api_key, base_url="https://api.deepseek.com")
                resp = client.chat.completions.create(
                    model="deepseek-chat",
                    messages=[{"role": "system", "content": "You are a disciplined value investor."}, {"role": "user", "content": prompt}],
                    max_tokens=1500,
                )
                st.markdown(resp.choices[0].message.content)
            except Exception as e:
                st.error(f"Failed: {e}")
    else:
        st.warning("Set DeepSeek API Key in sidebar")

# ---- Quick Actions ----
st.divider()
top3 = df_scored.head(3)
cols = st.columns(3)
for i, (_, row) in enumerate(top3.iterrows()):
    with cols[i]:
        st.markdown(f"**#{i+1} {row['code']} — {str(row['name'])[:25]}**")
        st.caption(f"VALUE+: {row['value_adj']:.3f} | 1Y: {row.get('return_1y','N/A')}")
        if st.button(f"📈 Detail", key=f"vdet_{row['code']}"):
            st.session_state["detail_fund_code"] = str(row["code"])
            st.session_state["detail_fund_name"] = str(row["name"])
            st.switch_page("pages/1_📈_Fund_Detail.py")
