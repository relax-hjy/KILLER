"""
数据源抽象基类

所有数据源必须实现此接口，保证上层调用的一致性。
切换到不同数据源时无需修改业务代码。
"""

from abc import ABC, abstractmethod
from dataclasses import dataclass
from datetime import date
from typing import Optional

import pandas as pd


@dataclass
class FundBasicInfo:
    """基金基础信息"""
    code: str           # 基金代码
    name: str           # 基金简称
    fund_type: str      # 基金类型（混合型/股票型/债券型等）
    company: str        # 基金公司
    establish_date: Optional[date] = None
    scale_yuan: Optional[float] = None  # 规模（元）


@dataclass
class NavRecord:
    """净值记录"""
    fund_code: str
    nav_date: date
    unit_nav: float         # 单位净值
    accum_nav: float        # 累计净值
    daily_return: float     # 日增长率（%）
    subscription_status: str = "开放"  # 申购状态


class DataSource(ABC):
    """数据源抽象基类"""

    name: str = "base"

    @abstractmethod
    def fetch_fund_list(self) -> pd.DataFrame:
        """
        获取全市场开放式基金列表

        Returns
        -------
        DataFrame with columns:
            code, name, fund_type, company, establish_date, scale_yuan,
            unit_nav, accum_nav, daily_return
        """
        ...

    @abstractmethod
    def fetch_nav_history(
        self,
        fund_code: str,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> pd.DataFrame:
        """
        获取单只基金历史净值

        Parameters
        ----------
        fund_code : str
            基金代码（如 "000001"）
        start_date : str, optional
            起始日期 "YYYY-MM-DD"
        end_date : str, optional
            截止日期 "YYYY-MM-DD"

        Returns
        -------
        DataFrame with columns:
            nav_date, unit_nav, accum_nav, daily_return
        """
        ...

    @abstractmethod
    def fetch_realtime_estimate(self) -> pd.DataFrame:
        """
        获取全市场基金实时估值（仅交易日盘中有效）

        Returns
        -------
        DataFrame with columns:
            code, name, unit_nav, estimate_nav, estimate_return, update_time
        """
        ...

    @abstractmethod
    def fetch_portfolio(self, fund_code: str, report_date: str) -> pd.DataFrame:
        """
        获取基金持仓明细（前十大重仓股）

        Parameters
        ----------
        fund_code : str
            基金代码
        report_date : str
            报告期 "2025Q1" / "2024Q4"

        Returns
        -------
        DataFrame with columns:
            stock_code, stock_name, weight_pct, market_value
        """
        ...

    def health_check(self) -> bool:
        """数据源连通性检查"""
        try:
            self.fetch_fund_list()
            return True
        except Exception:
            return False
