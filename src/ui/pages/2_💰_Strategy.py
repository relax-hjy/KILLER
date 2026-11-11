"""
Buy & Hold Strategy Simulator + AI Analysis
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

import pandas as pd
import numpy as np
import streamlit as st
import plotly.graph_objects as go
from datetime import date, timedelta

from src.data.akshare_source import AkShareSource
from src.db.repository import Repository
from src.screening.engine import FundScreener

st.set_page_config(page_title="Strategy & AI Analysis", page_icon="💰", layout="wide")

# ---- Fund Selection ----
fund_code = st.session_state.get("detail_fund_code", "")

st.header("💰 Buy & Hold Strategy Simulator")

col_code, col_name = st.columns([1, 2])
with col_code:
    manual_code = st.text_input("Fund Code", value=fund_code, placeholder="e.g. 000001")
with col_name:
    fund_name_display = st.text_input("Fund Name (optional)", value=st.session_state.get("detail_fund_name", ""))

if not manual_code:
    st.info("Enter a fund code to simulate, or click a fund from the Screener page")
    st.stop()

# ---- Data Source ----
@st.cache_resource
def get_datasource():
    return AkShareSource()

datasource = get_datasource()

# ---- Fetch History ----
with st.spinner(f"Fetching {manual_code} history..."):
    try:
        df_nav = datasource.fetch_nav_history(manual_code)
        if df_nav.empty:
            st.warning(f"No data for {manual_code}")
            st.stop()

        nav_series = df_nav["unit_nav"].astype(float)
        date_series = pd.to_datetime(df_nav["nav_date"])
        min_date = date_series.min().date()
        max_date = date_series.max().date()

        # ---- Date Range Picker ----
        st.subheader("📅 Investment Period")
        col_start, col_end, col_amount = st.columns(3)
        with col_start:
            buy_date = st.date_input("Buy Date", value=max_date - timedelta(days=365),
                                     min_value=min_date, max_value=max_date)
        with col_end:
            sell_date = st.date_input("Sell Date (or hold till now)", value=max_date,
                                      min_value=buy_date, max_value=max_date)
        with col_amount:
            invest_amount = st.number_input("Investment Amount (¥)", value=10000, min_value=100, step=1000)

        # ---- Filter to selected range ----
        mask = (date_series >= pd.Timestamp(buy_date)) & (date_series <= pd.Timestamp(sell_date))
        period_df = df_nav[mask].copy()
        if len(period_df) < 2:
            st.warning("Selected period has insufficient data")
            st.stop()

        period_df["unit_nav"] = period_df["unit_nav"].astype(float)
        buy_nav = period_df.iloc[0]["unit_nav"]
        sell_nav = period_df.iloc[-1]["unit_nav"]
        units = invest_amount / buy_nav
        final_value = units * sell_nav

        # ---- Results ----
        st.divider()
        st.subheader("📊 Simulation Results")

        total_return = (sell_nav / buy_nav - 1) * 100
        days_held = (sell_date - buy_date).days or 1
        annual_return = ((1 + total_return / 100) ** (365 / days_held) - 1) * 100
        profit = final_value - invest_amount

        col1, col2, col3, col4 = st.columns(4)
        col1.metric("Final Value", f"¥{final_value:,.2f}", delta=f"¥{profit:+,.2f}")
        col2.metric("Total Return", f"{total_return:.2f}%")
        col3.metric("Annualized Return", f"{annual_return:.2f}%")
        col4.metric("Holding Period", f"{days_held} days")

        col5, col6, col7, col8 = st.columns(4)
        col5.metric("Buy NAV", f"{buy_nav:.4f}")
        col6.metric("Sell NAV", f"{sell_nav:.4f}")
        col7.metric("Units Bought", f"{units:,.2f}")
        col8.metric("Days Held", f"{days_held}")

        # Max drawdown in period
        cummax = period_df["unit_nav"].cummax()
        dd = (period_df["unit_nav"] - cummax) / cummax
        max_dd_period = float(dd.min()) * 100
        st.metric("Max Drawdown (in period)", f"{max_dd_period:.2f}%")

        # ---- NAV Chart with Buy/Sell markers ----
        st.subheader("📈 NAV with Buy & Sell Markers")
        fig = go.Figure()

        # Full history in background (light)
        fig.add_trace(go.Scatter(
            x=df_nav["nav_date"], y=df_nav["unit_nav"].astype(float),
            mode="lines", name="Full History", line=dict(color="lightgray", width=0.5),
            showlegend=True,
        ))

        # Period NAV
        fig.add_trace(go.Scatter(
            x=period_df["nav_date"], y=period_df["unit_nav"],
            mode="lines", name="Holding Period", line=dict(color="#1f77b4", width=2),
        ))

        # Buy marker
        fig.add_trace(go.Scatter(
            x=[period_df.iloc[0]["nav_date"]], y=[buy_nav],
            mode="markers+text", name="BUY",
            marker=dict(color="green", size=14, symbol="triangle-up"),
            text=["BUY"], textposition="bottom center",
            showlegend=True,
        ))

        # Sell marker
        fig.add_trace(go.Scatter(
            x=[period_df.iloc[-1]["nav_date"]], y=[sell_nav],
            mode="markers+text", name="SELL",
            marker=dict(color="red", size=14, symbol="triangle-down"),
            text=["SELL"], textposition="top center",
            showlegend=True,
        ))

        fig.update_layout(
            xaxis_title="Date", yaxis_title="NAV",
            hovermode="x unified",
        )
        st.plotly_chart(fig, use_container_width=True)

        # ---- AI Analysis ----
        st.divider()
        st.subheader("🤖 AI Analysis & Recommendation")

        # Prepare analysis data
        all_nav = df_nav["unit_nav"].astype(float)
        all_ret = df_nav.get("daily_return", pd.Series(dtype=float)).astype(float)

        # Calculate key metrics for AI context
        if len(df_nav) >= 252:
            y1_start_nav = float(df_nav.iloc[-252]["unit_nav"])
            y1_return = (float(all_nav.iloc[-1]) / y1_start_nav - 1) * 100
        else:
            y1_return = None

        max_dd_all = FundScreener.calc_max_drawdown(all_nav) * 100
        sharpe_all = FundScreener.calc_sharpe_ratio(all_ret)

        # Recent trend
        if len(df_nav) >= 60:
            recent_nav = all_nav.iloc[-60:]
            short_trend = "up" if recent_nav.iloc[-1] > recent_nav.iloc[0] else "down"
            recent_return = (float(recent_nav.iloc[-1]) / float(recent_nav.iloc[0]) - 1) * 100
        else:
            short_trend = "flat"
            recent_return = 0

        # Volatility (std of daily returns)
        if len(all_ret) >= 20:
            volatility = float(all_ret.tail(252).std()) if len(all_ret) >= 252 else float(all_ret.std())
        else:
            volatility = 0

        # Data summary for AI
        data_summary = f"""
## Fund Analysis Data

**Basic Info:**
- Fund Code: {manual_code}
- Fund Name: {fund_name_display or 'N/A'}
- Data Range: {min_date} to {max_date} ({len(df_nav)} records)
- Latest NAV: {float(all_nav.iloc[-1]):.4f}

**Risk Metrics (Full History):**
- Max Drawdown: {max_dd_all:.2f}%
- Sharpe Ratio (approx): {sharpe_all:.2f}
- Daily Volatility (1Y): {volatility:.2f}%

**Return Metrics:**
- 1Y Return: {y1_return:.2f}% (if available)
- Recent 60-day trend: {short_trend} ({recent_return:.2f}%)

**Buy & Hold Simulation:**
- Buy Date: {buy_date} @ NAV {buy_nav:.4f}
- Sell Date: {sell_date} @ NAV {sell_nav:.4f}
- Holding Period: {days_held} days
- Total Return: {total_return:.2f}%
- Annualized Return: {annual_return:.2f}%
- Max Drawdown (in period): {max_dd_period:.2f}%
- Investment: ¥{invest_amount:,.0f} → Final: ¥{final_value:,.2f} (Profit: ¥{profit:+,.2f})

Please analyze this fund's performance, risk level, and provide a recommendation on whether to buy/hold/sell.
Focus on: risk-adjusted return, drawdown risk, recent momentum, and suitability for long-term investors.
Give a clear recommendation with reasoning in both English and Chinese.
"""

        if st.button("🤖 Run AI Analysis", use_container_width=True, type="primary"):
            with st.spinner("Analyzing fund performance..."):
                # Use Anthropic Claude for analysis
                import os
                from openai import OpenAI

                api_key = os.environ.get("DEEPSEEK_API_KEY")
                if not api_key:
                    st.warning(
                        "DeepSeek API key not set. Add it in the sidebar.\n\n"
                        "Showing automated analysis instead:"
                    )
                    # Fallback: rule-based analysis
                    verdict = "BUY ✅" if total_return > 10 and max_dd_period > -15 else (
                        "HOLD ⏸️" if total_return > 0 else "SELL ❌"
                    )
                    st.info(f"**Automated Signal: {verdict}**")
                    st.markdown(f"""
                    **Quick Analysis (rule-based):**
                    - Total Return: {total_return:.2f}% → {"Strong" if total_return > 20 else "Moderate" if total_return > 5 else "Weak"}
                    - Risk: Max Drawdown {max_dd_period:.2f}% → {"High Risk" if max_dd_period < -20 else "Moderate Risk" if max_dd_period < -10 else "Low Risk"}
                    - Long-term Sharpe: {sharpe_all:.2f} → {"Good risk-adjusted return" if sharpe_all > 0.5 else "Below average risk-adjusted return"}
                    - 60-day Momentum: {short_trend} ({recent_return:.2f}%)

                    **Recommendation:** {verdict}
                    """)
                else:
                    try:
                        client = OpenAI(api_key=api_key, base_url="https://api.deepseek.com")
                        response = client.chat.completions.create(
                            model="deepseek-chat",
                            messages=[
                                {"role": "system", "content": "You are a professional fund analyst. Analyze the data and give clear buy/hold/sell recommendations with reasoning."},
                                {"role": "user", "content": data_summary},
                            ],
                            max_tokens=2000,
                        )
                        st.markdown(response.choices[0].message.content)
                    except Exception as e:
                        st.error(f"AI analysis failed: {e}")
                        st.text_area("Analysis Data (for manual review)", data_summary, height=300)

        # Always show data summary
        with st.expander("📋 Analysis Data"):
            st.text(data_summary)

    except Exception as e:
        st.error(f"Failed to load: {e}")
