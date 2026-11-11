"""
AkShare 数据源实现

底层数据来自东方财富/天天基金网公开页面。
接口稳定但需注意：依赖网页结构，大版本升级前先看 changelog。
"""

from typing import Optional

import pandas as pd
from loguru import logger

from src.data.base import DataSource


class AkShareSource(DataSource):
    """AkShare 数据源 — 基金数据主通道"""

    name = "akshare"

    def fetch_fund_list(self) -> pd.DataFrame:
        """
        获取全市场开放式基金排名数据

        合并两个 AkShare 接口：
        - fund_open_fund_rank_em():  净值 + 日涨跌 + 阶段收益率（近1周/1月/3月/6月/1年/2年/3年）
        - fund_name_em():            基金类型分类
        返回全量基金列表，约 19000+ 只。
        """
        import akshare as ak

        logger.info("[AkShare] 拉取全市场基金排名 ...")
        try:
            # 1. 拉取排名数据（含净值、阶段收益、日增长率）
            df_rank = ak.fund_open_fund_rank_em(symbol="全部")
            logger.info(f"[AkShare] 排名数据: {len(df_rank)} 条")

            # 2. 拉取基金类型数据
            df_type = ak.fund_name_em()
            logger.info(f"[AkShare] 基金类型: {len(df_type)} 条")

            # 3. 合并：左连接保留排名数据，补充基金类型
            df_type = df_type[["基金代码", "基金类型"]].drop_duplicates(subset="基金代码")
            df = df_rank.merge(df_type, on="基金代码", how="left")
            logger.info(f"[AkShare] 合并后: {len(df)} 条（含基金类型）")

            # 4. 列名规范化：中文 → 英文，合并后统一重命名
            # fund_open_fund_rank_em 返回列：
            # 序号, 基金代码, 基金简称, 日期, 单位净值, 累计净值, 日增长率,
            # 近1周, 近1月, 近3月, 近6月, 近1年, 近2年, 近3年, 今年来, 成立来, 自定义, 手续费
            col_mapping = {
                "基金代码": "code",
                "基金简称": "name",
                "基金类型": "fund_type",
                "日期": "nav_date",
                "单位净值": "unit_nav",
                "累计净值": "accum_nav",
                "日增长率": "daily_return",
                "近1周": "return_1w",
                "近1月": "return_1m",
                "近3月": "return_3m",
                "近6月": "return_6m",
                "近1年": "return_1y",
                "近2年": "return_2y",
                "近3年": "return_3y",
                "今年来": "return_ytd",
                "成立来": "return_since_inception",
            }
            df = df.rename(columns={k: v for k, v in col_mapping.items() if k in df.columns})

            # 规模无法从免费 API 直接获取
            df["scale_yuan"] = None

            logger.success(f"[AkShare] 获取到 {len(df)} 条基金数据（含类型、净值、阶段收益）")
            return df
        except Exception as e:
            logger.error(f"[AkShare] 基金列表拉取失败: {e}")
            raise

    def fetch_nav_history(
        self,
        fund_code: str,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> pd.DataFrame:
        """
        获取单只基金历史净值走势

        AkShare 接口返回该基金全部历史数据，
        如有日期范围需求，由调用方自行在 DataFrame 上筛选。
        """
        import akshare as ak

        logger.info(f"[AkShare] 拉取基金 {fund_code} 历史净值 ...")
        try:
            df = ak.fund_open_fund_info_em(
                symbol=fund_code,
                indicator="单位净值走势",
            )
            if df is None or df.empty:
                logger.warning(f"[AkShare] 基金 {fund_code} 无数据")
                return pd.DataFrame()

            # 统一列名（新版 API 不再返回累计净值）
            df = df.rename(columns={
                "净值日期": "nav_date",
                "单位净值": "unit_nav",
                "日增长率": "daily_return",
            })

            # 日期筛选
            if "nav_date" in df.columns:
                df["nav_date"] = pd.to_datetime(df["nav_date"])
                if start_date:
                    df = df[df["nav_date"] >= pd.to_datetime(start_date)]
                if end_date:
                    df = df[df["nav_date"] <= pd.to_datetime(end_date)]

            logger.success(f"[AkShare] 基金 {fund_code}: {len(df)} 条净值记录")
            return df.sort_values("nav_date")

        except Exception as e:
            logger.error(f"[AkShare] 基金 {fund_code} 净值拉取失败: {e}")
            raise

    def fetch_realtime_estimate(self) -> pd.DataFrame:
        """获取全市场基金实时估值"""
        import akshare as ak

        logger.info("[AkShare] 拉取基金实时估值 ...")
        try:
            df = ak.fund_value_estimation_em()
            logger.success(f"[AkShare] 获取到 {len(df)} 条实时估值")
            return df
        except Exception as e:
            logger.error(f"[AkShare] 实时估值拉取失败: {e}")
            raise

    def fetch_portfolio(self, fund_code: str, report_date: str) -> pd.DataFrame:
        """
        获取基金持仓明细

        Parameters
        ----------
        fund_code : str
            基金代码
        report_date : str
            报告期，如 "2025Q1"
        """
        import akshare as ak

        logger.info(f"[AkShare] 拉取基金 {fund_code} 持仓（{report_date}）...")
        try:
            # 解析 "2025Q1" → year="2025"
            year = report_date[:4]
            df = ak.fund_portfolio_hold_em(fund=fund_code, date=year)
            logger.success(f"[AkShare] 基金 {fund_code}: {len(df)} 条持仓")
            return df
        except Exception as e:
            logger.error(f"[AkShare] 基金 {fund_code} 持仓拉取失败: {e}")
            raise
