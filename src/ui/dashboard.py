"""
Fund Screener & Monitor — Streamlit Dashboard

Usage:
    streamlit run src/ui/dashboard.py

Pages:
    1. Fund Screener — multi-dimension screening + ranking
    2. Fund Detail  — NAV chart + return trend + metrics (standalone page)
    3. Watchlist    — monitored funds + alerts
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import pandas as pd
import streamlit as st
import plotly.express as px
import plotly.graph_objects as go

from src.data.akshare_source import AkShareSource
from src.db.repository import Repository
from src.screening.engine import FundScreener, ScreenCriteria
from src.monitoring.watchdog import FundWatchdog

# ---- Page Config ----
st.set_page_config(
    page_title="Fund Killer — Screener & Monitor",
    page_icon="🔍",
    layout="wide",
)

st.title("🔍 Fund Killer — Screener & Monitor")

# ---- Sidebar: API Key ----
with st.sidebar:
    st.subheader("🔑 API Key")
    api_key = st.text_input(
        "DeepSeek API Key",
        type="password",
        value=st.session_state.get("deepseek_key", ""),
        placeholder="sk-...",
        help="Paste your DeepSeek API key to enable AI analysis features",
    )
    if api_key:
        st.session_state["deepseek_key"] = api_key
        st.success("✅ Key set — AI features enabled")
        import os
        os.environ["DEEPSEEK_API_KEY"] = api_key
    else:
        st.info("Enter DeepSeek API key to enable AI")

# ---- Resources ----
@st.cache_resource
def get_datasource():
    return AkShareSource()

@st.cache_resource
def get_repo():
    return Repository()

datasource = get_datasource()
repo = get_repo()

@st.cache_data(ttl=300, show_spinner=False)
def load_funds_for_screen():
    return repo.get_funds_df()

# ---- Tabs ----
tab1, tab2, tab3 = st.tabs(["📊 Screener", "📈 Lookup", "⚠️ Watchlist"])

# ==========================================================
# Tab 1: Fund Screener
# ==========================================================
with tab1:
    st.header("Multi-Dimension Fund Screener")

    col1, col2, col3, col4 = st.columns(4)
    with col1:
        fund_types = st.multiselect(
            "Fund Type",
            ["Hybrid", "Equity", "Bond", "Money Market", "QDII", "FOF", "Index"],
            default=["Hybrid", "Equity"],
        )
    with col2:
        min_return = st.slider("Min 1Y Return (%)", 0.0, 50.0, 0.0, 1.0)
    with col3:
        max_dd = st.slider("Max Drawdown (%)", 0.0, 50.0, 20.0, 1.0)
    with col4:
        sort_by_label = st.selectbox("Sort By", ["1Y Return", "6M Return", "3M Return", "1M Return"])
        top_n = st.slider("Top N", 10, 100, 30)

    sort_map = {"1Y Return": "return_1y", "6M Return": "return_6m",
                "3M Return": "return_3m", "1M Return": "return_1m"}

    # Map English fund type selections to Chinese patterns for fuzzy matching
    type_pattern_map = {
        "Hybrid": "混合",
        "Equity": "股票",
        "Bond": "债券",
        "Money Market": "货币",
        "QDII": "QDII",
        "FOF": "FOF",
        "Index": "指数",
    }
    cn_fund_types = [type_pattern_map.get(t, t) for t in fund_types]

    # Sync button with last sync time
    last_sync = repo.get_last_sync_time()
    sync_label = "🔄 Sync Data (from AkShare)"
    if last_sync:
        sync_label += f"  —  last: {last_sync.strftime('%Y-%m-%d %H:%M')}"

    col_btn1, col_btn2 = st.columns(2)
    with col_btn1:
        if st.button(sync_label, use_container_width=True):
            with st.spinner("Fetching all fund data from AkShare (~6 sec)..."):
                try:
                    df = datasource.fetch_fund_list()
                    repo.upsert_funds(df)
                    st.success(f"Synced {len(df)} funds to database")
                    st.rerun()
                except Exception as e:
                    st.error(f"Sync failed: {e}")

    with col_btn2:
        if st.button("🔍 Screen from Database", use_container_width=True):
            st.session_state["screening_done"] = True

    if st.session_state.get("screening_done"):
        with st.spinner("Querying local database..."):
            try:
                df = load_funds_for_screen()
                if df.empty:
                    st.warning("Database is empty — click 'Sync Data' first")
                else:
                    screener = FundScreener(df)
                    criteria = ScreenCriteria(
                        fund_types=cn_fund_types if cn_fund_types else [],
                        min_return_1y=min_return,
                        max_drawdown=max_dd / 100.0,
                        sort_by=sort_map.get(sort_by_label, "return_1y"),
                        top_n=top_n,
                    )
                    result = screener.screen(criteria)

                    st.subheader(f"Results: {len(result)} funds")
                    cols_display = [c for c in ["code", "name", "fund_type", "unit_nav", "daily_return",
                                                 "return_1w", "return_1m", "return_3m", "return_6m", "return_1y"]
                                    if c in result.columns]
                    if cols_display:
                        selection = st.dataframe(
                            result[cols_display].head(top_n),
                            use_container_width=True,
                            hide_index=True,
                            selection_mode="single-row",
                            on_select="rerun",
                            key="result_table",
                        )

                        if "return_1y" in result.columns:
                            fig = px.histogram(
                                result.head(top_n),
                                x="return_1y", nbins=20,
                                title=f"Top {min(top_n, len(result))} Funds — 1Y Return Distribution",
                                labels={"return_1y": "1Y Return (%)"},
                            )
                            st.plotly_chart(fig, use_container_width=True)

                        # Click row → jump to detail page
                        if selection is not None and selection.selection and selection.selection.rows:
                            row_idx = selection.selection.rows[0]
                            selected_row = result.iloc[row_idx]
                            st.session_state["detail_fund_code"] = str(selected_row["code"])
                            st.session_state["detail_fund_name"] = str(selected_row.get("name", ""))
                            st.switch_page("pages/1_📈_Fund_Detail.py")

            except Exception as e:
                st.error(f"Screening failed: {e}")


# ==========================================================
# Tab 2: Fund Lookup
# ==========================================================
with tab2:
    st.header("Fund Lookup")
    fund_code = st.text_input("Fund Code (e.g. 000001)", "000001",
                              help="Enter a 6-digit fund code and click to view full detail")
    if st.button("📈 View Detail"):
        st.session_state["detail_fund_code"] = fund_code
        st.session_state["detail_fund_name"] = ""
        st.switch_page("pages/1_📈_Fund_Detail.py")


# ==========================================================
# Tab 3: Watchlist
# ==========================================================
with tab3:
    st.header("⚠️ Watchlist Monitor")

    watch_list = repo.get_watch_list()
    if not watch_list:
        st.info("No watched funds yet — add from the Fund Detail page!")
    else:
        st.subheader(f"Watching: {len(watch_list)} funds")

        col1, col2 = st.columns(2)
        with col1:
            if st.button("🔄 Check Now", use_container_width=True):
                watchdog = FundWatchdog(repo, datasource)
                with st.spinner("Checking..."):
                    alerts = watchdog.sync_and_check()
                    if alerts:
                        for a in alerts:
                            st.warning(str(a))
                    else:
                        st.success("✅ All watched funds are normal")

        with col2:
            if st.button("🗑️ Clear Watchlist", use_container_width=True):
                for item in watch_list:
                    repo.remove_from_watch(item.fund_code)
                st.rerun()

        # Watchlist items
        st.divider()
        for item in watch_list:
            with st.expander(f"{item.fund_code} — {item.fund_name or 'Unknown'}"):
                try:
                    df = datasource.fetch_nav_history(item.fund_code)
                    if not df.empty:
                        latest = df.iloc[-1]
                        c1, c2, c3 = st.columns(3)
                        c1.metric("NAV", f"{latest['unit_nav']:.4f}")
                        c2.metric("Date", str(latest['nav_date']))
                        dr = float(latest.get("daily_return", 0))
                        c3.metric("Daily Change", f"{dr:.2f}%", delta=f"{dr:.2f}%")

                        fig = go.Figure()
                        recent = df.tail(60)
                        fig.add_trace(go.Scatter(
                            x=recent["nav_date"],
                            y=recent["unit_nav"],
                            mode="lines",
                            name=item.fund_code,
                        ))
                        fig.update_layout(height=200, margin=dict(l=0, r=0, t=0, b=0))
                        st.plotly_chart(fig, use_container_width=True)

                        if st.button(f"Remove {item.fund_code}", key=f"remove_{item.fund_code}"):
                            repo.remove_from_watch(item.fund_code)
                            st.rerun()
                except Exception as e:
                    st.error(f"Failed to fetch {item.fund_code}: {e}")
