"""
Fund Detail — NAV / Return / Valuation / History Table
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

import pandas as pd
import streamlit as st
import plotly.graph_objects as go

from src.data.akshare_source import AkShareSource
from src.db.repository import Repository
from src.screening.engine import FundScreener

st.set_page_config(page_title="Fund Detail", page_icon="📈", layout="wide")

from config.strategies import PERIODS_FOR_DETAIL as PERIODS

fund_code = st.session_state.get("detail_fund_code", "")
fund_name = st.session_state.get("detail_fund_name", "")

col_input, col_back = st.columns([3, 1])
with col_input:
    manual_code = st.text_input("Fund Code", value=fund_code, placeholder="e.g. 000001")
with col_back:
    st.caption("")
    if st.button("← Back to Screening"):
        st.switch_page("dashboard.py")

if not manual_code:
    st.info("Click a fund row from the screening page, or enter a fund code manually")
    st.stop()

@st.cache_resource
def get_datasource():
    return AkShareSource()

datasource = get_datasource()

with st.spinner(f"Fetching {manual_code} historical NAV..."):
    try:
        df_nav = datasource.fetch_nav_history(manual_code)
        if df_nav.empty:
            st.warning(f"No data for fund {manual_code}")
            st.stop()

        nav_all = df_nav["unit_nav"].astype(float)
        current_nav = float(nav_all.iloc[-1])
        current_date = str(df_nav.iloc[-1]["nav_date"])

        # ================================================================
        # LEFT COLUMN — Charts
        # ================================================================
        st.subheader(f"📈 {manual_code} — {fund_name}")

        col_left, col_right = st.columns([2, 1])

        with col_left:
            tab_nav, tab_return, tab_holdings, tab_table = st.tabs([
                "NAV Chart", "Return Chart", "Holdings & Sectors", "History Table",
            ])

            # ---- NAV Chart ----
            with tab_nav:
                period_labels = [p[0] for p in PERIODS]
                sel = st.radio("Time Period", period_labels, horizontal=True, index=3, key="nav_period")
                days = dict(PERIODS).get(sel)
                chart_df = df_nav.iloc[-days:] if days and len(df_nav) > days else df_nav

                fig = go.Figure()
                fig.add_trace(go.Scatter(
                    x=chart_df["nav_date"], y=chart_df["unit_nav"].astype(float),
                    mode="lines", name="NAV", line=dict(color="#1f77b4"),
                ))
                fig.update_layout(title=f"NAV — {sel}", xaxis_title="Date", yaxis_title="NAV", hovermode="x unified")
                st.plotly_chart(fig, use_container_width=True)

            # ---- Return Chart ----
            with tab_return:
                sel2 = st.radio("Time Period", period_labels, horizontal=True, index=3, key="ret_period")
                days2 = dict(PERIODS).get(sel2)
                ret_df = (df_nav.iloc[-days2:] if days2 and len(df_nav) > days2 else df_nav).copy()

                if len(ret_df) > 1:
                    base_nav = float(ret_df.iloc[0]["unit_nav"])
                    ret_df["cum_return"] = (ret_df["unit_nav"].astype(float) / base_nav - 1) * 100

                    fig = go.Figure()
                    fig.add_trace(go.Scatter(
                        x=ret_df["nav_date"], y=ret_df["cum_return"],
                        mode="lines", name="Cum. Return (%)",
                        line=dict(color="#ff7f0e", width=2),
                        fill="tozeroy", fillcolor="rgba(255,127,14,0.1)",
                    ))
                    fig.add_hline(y=0, line_dash="dash", line_color="gray", annotation_text="Baseline")
                    fig.update_layout(
                        title=f"Return — {sel2} (Base: {ret_df.iloc[0]['nav_date']})",
                        xaxis_title="Date", yaxis_title="Cumulative Return (%)", hovermode="x unified",
                    )
                    st.plotly_chart(fig, use_container_width=True)

                    total_ret = float(ret_df.iloc[-1]["cum_return"])
                    peak_ret = float(ret_df["cum_return"].max())
                    trough_ret = float(ret_df["cum_return"].min())
                    c1, c2, c3 = st.columns(3)
                    c1.metric("Total Return", f"{total_ret:.2f}%")
                    c2.metric("Peak Return", f"{peak_ret:.2f}%")
                    c3.metric("Max DD (period)", f"{trough_ret:.2f}%")

            # ---- Holdings & Sectors ----
            with tab_holdings:
                import akshare as ak

                try:
                    # Fetch holdings & industry data
                    df_hold = ak.fund_portfolio_hold_em(symbol=manual_code, date='2026')
                    df_ind = ak.fund_portfolio_industry_allocation_em(symbol=manual_code, date='2026')

                    if not df_hold.empty:
                        st.subheader("🏢 Top Holdings")
                        hold_display = df_hold.copy()
                        hold_cols_map = {
                            "股票代码": "Stock Code", "股票名称": "Stock Name",
                            "占净值比例": "Weight (%)", "持股数": "Shares (10k)",
                            "持仓市值": "Market Value (10k)",
                        }
                        available_hold_cols = [c for c in hold_cols_map if c in hold_display.columns]
                        hold_display = hold_display[available_hold_cols].rename(columns={
                            k: v for k, v in hold_cols_map.items() if k in available_hold_cols
                        })
                        st.dataframe(hold_display, use_container_width=True, hide_index=True)

                        # Top holding weight bar
                        if "Weight (%)" in hold_display.columns:
                            fig = go.Figure()
                            top5 = hold_display.head(5).sort_values("Weight (%)")
                            fig.add_trace(go.Bar(
                                y=top5["Stock Name"], x=top5["Weight (%)"],
                                orientation="h", marker=dict(color="#1f77b4"),
                            ))
                            fig.update_layout(title="Top 5 Holdings Weight", yaxis_title="")
                            st.plotly_chart(fig, use_container_width=True)

                    if not df_ind.empty:
                        st.subheader("🏭 Industry Allocation")
                        ind_display = df_ind.copy()
                        ind_cols_map = {
                            "行业名称": "Industry", "占净值比例": "Weight (%)",
                            "市值": "Market Value (10k)",
                        }
                        available_ind = [c for c in ind_cols_map if c in ind_display.columns]
                        ind_display = ind_display[available_ind].rename(columns={
                            k: v for k, v in ind_cols_map.items() if k in available_ind
                        })

                        # Pie chart
                        if "Weight (%)" in ind_display.columns:
                            fig = go.Figure()
                            fig.add_trace(go.Pie(
                                labels=ind_display["Industry"], values=ind_display["Weight (%)"],
                                hole=0.4, textinfo="label+percent",
                            ))
                            fig.update_layout(title="Sector Allocation")
                            st.plotly_chart(fig, use_container_width=True)

                        # Table
                        st.dataframe(ind_display, use_container_width=True, hide_index=True)

                        # ---- AI Sector Outlook ----
                        st.divider()
                        st.subheader("🤖 AI Sector Outlook")
                        if st.button("Analyze Industry Outlook", key="ai_sector",
                                     help="Uses AI to evaluate the fund's sector allocation, concentration risk, and industry prospects"):
                            with st.spinner("Analyzing industry outlook..."):
                                import os
                                from openai import OpenAI

                                holdings_str = df_hold[["股票名称", "占净值比例"]].to_string(index=False) if not df_hold.empty else "N/A"
                                ind_str = df_ind[["行业名称", "占净值比例"]].to_string(index=False) if not df_ind.empty else "N/A"

                                system_prompt = """You are a professional fund analyst specializing in industry research and portfolio analysis.
Analyze this fund's holdings and sector allocation:

Fund: """ + f"{manual_code} {fund_name}\n\n" + f"""Top Holdings:
{holdings_str}

Industry Allocation:
{ind_str}

Please analyze:
1. Concentration Risk: Is the fund too concentrated in specific stocks or sectors?
2. Industry Outlook: Growth prospects for top sectors - macro trends, policy support, cyclical position
3. Portfolio Quality: Top holdings quality (industry leaders, growth/value style, valuation)
4. Risk Factors: Key risks to watch
5. Overall Assessment: Rating (Strong Buy / Buy / Hold / Reduce / Sell)

Keep it concise. Reply in both English and Chinese."""

                                api_key = os.environ.get("DEEPSEEK_API_KEY")
                                if api_key:
                                    try:
                                        client = OpenAI(api_key=api_key, base_url="https://api.deepseek.com")
                                        resp = client.chat.completions.create(
                                            model="deepseek-chat",
                                            messages=[
                                                {"role": "system", "content": system_prompt},
                                            ],
                                            max_tokens=2000,
                                        )
                                        st.markdown(resp.choices[0].message.content)
                                    except Exception as e:
                                        st.error(f"AI analysis failed: {e}")
                                else:
                                    st.warning(
                                        "DeepSeek API key not set. Add it in the sidebar.\n\n"
                                        "Automated analysis:"
                                    )
                                    # Rule-based fallback
                                    if not df_ind.empty:
                                        top_sector = ind_display.iloc[0]["Industry"]
                                        top_weight = float(ind_display.iloc[0]["Weight (%)"])
                                    else:
                                        top_sector, top_weight = "Unknown", 0

                                    concentration = "High" if top_weight > 40 else ("Moderate" if top_weight > 25 else "Low")
                                    st.info(f"""
**Automated Assessment:**
- Top sector: **{top_sector}** ({top_weight}%)
- Concentration: **{concentration}**
- Sectors >20% allocation typically indicate concentrated risk
- Diversified across {len(df_ind)} sectors → {"Well-diversified" if len(df_ind) >= 5 else "Concentrated portfolio"}

**Recommendation**: Check individual stock quality and sector valuations in the Detail page.
""")

                    elif df_hold.empty:
                        st.info("No holdings data available. Some funds (e.g., bond/money market) don't disclose stock holdings.")

                except Exception as e:
                    st.warning(f"Holdings data not available: {e}")

            # ---- History Table ----
            with tab_table:
                st.caption(f"{len(df_nav)} records")
                tbl = df_nav[["nav_date", "unit_nav", "daily_return"]].sort_values(
                    "nav_date", ascending=False
                ).head(500).copy()
                tbl["unit_nav"] = tbl["unit_nav"].astype(float).round(4)
                tbl["daily_return"] = tbl["daily_return"].astype(float).round(2)
                tbl = tbl.rename(columns={
                    "nav_date": "Date", "unit_nav": "Unit NAV", "daily_return": "Daily Return (%)",
                })
                st.dataframe(tbl, use_container_width=True, hide_index=True)

        # ================================================================
        # RIGHT COLUMN — Metrics + Valuation
        # ================================================================
        with col_right:
            st.subheader("Key Metrics")
            st.metric("Latest NAV", f"{current_nav:.4f}")
            st.metric("NAV Date", current_date)
            dr_val = float(df_nav.iloc[-1].get("daily_return", 0))
            st.metric("Daily Change", f"{dr_val:.2f}%", delta=f"{dr_val:.2f}%")

            # ---- VALUATION WATER LEVEL ----
            if len(nav_all) >= 20:
                all_time_high = float(nav_all.max())
                all_time_low = float(nav_all.min())
                pct_rank = float((nav_all < current_nav).sum() / len(nav_all) * 100)
                dist_ath = (current_nav / all_time_high - 1) * 100

                # 52-week stats
                if len(nav_all) >= 252:
                    y1 = nav_all.tail(252)
                    y1_high = float(y1.max())
                    y1_low = float(y1.min())
                    dist_y1_high = (current_nav / y1_high - 1) * 100
                    y1_pct = float((y1 < current_nav).sum() / 252 * 100)
                else:
                    y1_high = y1_low = dist_y1_high = y1_pct = None

                # Moving averages
                if len(nav_all) >= 50:
                    ma50 = float(nav_all.tail(50).mean())
                    ma50_pct = (current_nav / ma50 - 1) * 100
                else:
                    ma50 = ma50_pct = None
                if len(nav_all) >= 200:
                    ma200 = float(nav_all.tail(200).mean())
                    ma200_pct = (current_nav / ma200 - 1) * 100
                else:
                    ma200 = ma200_pct = None

                # Valuation zone
                if pct_rank >= 90:
                    zone, zcolor, zdesc = "🔴 Overvalued", "red", "Near all-time high. High correction risk."
                elif pct_rank >= 70:
                    zone, zcolor, zdesc = "🟡 Elevated", "orange", "Above normal. Caution advised."
                elif pct_rank >= 30:
                    zone, zcolor, zdesc = "🟢 Fair Value", "green", "Within normal range."
                elif pct_rank >= 10:
                    zone, zcolor, zdesc = "🟢 Undervalued", "blue", "Below average. Possible opportunity."
                else:
                    zone, zcolor, zdesc = "🟢 Deep Value", "darkblue", "Near all-time low."

                st.divider()
                st.subheader("⚠️ Valuation")
                st.markdown(f"**:{zcolor}[{zone}]**")
                st.caption(zdesc)

                c1, c2 = st.columns(2)
                c1.metric("Hist. Percentile", f"{pct_rank:.0f}%",
                          delta="High" if pct_rank > 70 else ("Normal" if pct_rank > 30 else "Low"),
                          help="Where current NAV ranks in full history. 90% = more expensive than 90% of all past prices (overvalued). 10% = cheaper than 90% of past prices (undervalued).")
                c2.metric("Dist. from ATH", f"{dist_ath:.2f}%",
                          help="How far current NAV is from the all-time high. -10% = 10% below peak.")

                if y1_pct is not None:
                    c3, c4 = st.columns(2)
                    c3.metric("52W Percentile", f"{y1_pct:.0f}%",
                              help="Where current NAV ranks in the past 52 weeks. Above 80% = near 1-year high.")
                    c4.metric("Dist. from 52W High", f"{dist_y1_high:.2f}%",
                              help="How far from the 52-week high. -5% = currently 5% below the 1-year peak.")

                if ma50 is not None:
                    st.metric("vs MA50 (50-day avg)", f"{ma50_pct:+.2f}%",
                              delta="Above MA50 ↑" if ma50_pct > 0 else "Below MA50 ↓",
                              help="Deviation from 50-day moving average. Positive = recent uptrend, Negative = recent downtrend.")
                if ma200 is not None:
                    signal = "Bullish ↑" if ma200_pct > 0 else "Bearish ↓"
                    st.metric("vs MA200 (200-day avg)", f"{ma200_pct:+.2f}%",
                              delta=signal,
                              help="Deviation from 200-day moving average. Above = long-term uptrend. Below = long-term downtrend.")

                # Risk metrics
                st.divider()
                st.caption("Risk Metrics")
                ret_series = df_nav.get("daily_return", pd.Series(dtype=float)).astype(float)
                max_dd = FundScreener.calc_max_drawdown(nav_all)
                sharpe = FundScreener.calc_sharpe_ratio(ret_series)
                st.metric("Max Drawdown (Hist)", f"{max_dd*100:.2f}%")
                st.metric("Sharpe Ratio (Ann.)", f"{sharpe:.2f}")

            # Period returns
            if len(nav_all) >= 250:
                st.divider()
                st.caption("Period Returns (from history)")
                for d, lbl in [(21, "1M"), (63, "3M"), (126, "6M"), (252, "1Y")]:
                    if len(nav_all) >= d:
                        r = (current_nav / float(nav_all.iloc[-d]) - 1) * 100
                        st.metric(lbl, f"{r:.2f}%")

        # ---- Add to watchlist ----
        st.divider()
        col_w, _ = st.columns([1, 3])
        with col_w:
            @st.cache_resource
            def get_repo():
                return Repository()
            repo = get_repo()
            if st.button("⭐ Add to Watchlist", use_container_width=True):
                repo.add_to_watch(manual_code, fund_name=fund_name or f"Fund {manual_code}")
                st.toast(f"Added {manual_code} to watchlist!")

    except Exception as e:
        st.error(f"Failed to load detail: {e}")
