"""
基金筛选引擎

支持多维度组合筛选：
- 收益率排名（1月/3月/6月/1年）
- 最大回撤限制
- 夏普比率
- 基金类型过滤
- 规模过滤

所有指标支持动态排序和阈值过滤。
"""

from dataclasses import dataclass, field
from typing import Optional

import numpy as np
import pandas as pd
from loguru import logger


@dataclass
class ScreenCriteria:
    """筛选条件"""

    # 基金类型（空列表 = 不限）
    fund_types: list[str] = field(default_factory=list)

    # 规模（元）— 最小和最大
    min_scale: Optional[float] = None
    max_scale: Optional[float] = None

    # 收益率阈值（%）— 各周期最低要求
    min_return_1m: Optional[float] = None
    min_return_3m: Optional[float] = None
    min_return_6m: Optional[float] = None
    min_return_1y: Optional[float] = None

    # 最大回撤上限（小数，如 0.20 表示 20%）
    max_drawdown: Optional[float] = None

    # 夏普比率下限
    min_sharpe: Optional[float] = None

    # 排序
    sort_by: str = "return_1y"   # return_1m / return_3m / return_6m / return_1y / max_drawdown / sharpe_ratio
    sort_ascending: bool = False  # 收益率类指标默认降序

    # 分页
    top_n: int = 50
    skip: int = 0


class FundScreener:
    """基金筛选器"""

    def __init__(self, df: pd.DataFrame):
        """
        Parameters
        ----------
        df : pd.DataFrame
            基金数据表，至少包含:
            code, name, fund_type, scale_yuan, unit_nav, daily_return,
            return_1m, return_3m, return_6m, return_1y,
            max_drawdown, sharpe_ratio
        """
        self.df = df.copy()
        self._normalize_columns()

    def _normalize_columns(self):
        """统一列名（中文 → 英文）"""
        mapping = {
            "基金代码": "code",
            "基金简称": "name",
            "基金类型": "fund_type",
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
        }
        self.df = self.df.rename(columns={k: v for k, v in mapping.items() if k in self.df.columns})

        # 确保必要列存在
        for col in ["code", "name"]:
            if col not in self.df.columns:
                raise ValueError(f"DataFrame 缺少必要列: {col}")

    def screen(self, criteria: ScreenCriteria) -> pd.DataFrame:
        """
        按条件筛选基金

        Returns
        -------
        pd.DataFrame : 筛选结果（按指定指标排序，取 top_n）
        """
        df = self.df.copy()
        initial_count = len(df)
        filters_applied: list[str] = []

        # 1. 基金类型过滤（模糊匹配：用户选"混合型"可匹配"混合型-偏股"等子类）
        if criteria.fund_types:
            type_pattern = "|".join(criteria.fund_types)
            df = df[df["fund_type"].str.contains(type_pattern, na=False)]
            filters_applied.append(f"类型={criteria.fund_types}")

        # 2. 规模过滤（缺失数据不参与过滤）
        if criteria.min_scale is not None:
            col = self._find_col("scale_yuan", "规模")
            if col and col in df.columns:
                mask = df[col].isna() | (df[col] >= criteria.min_scale * 1e8)
                df = df[mask]
                filters_applied.append(f"规模≥{criteria.min_scale}亿")
        if criteria.max_scale is not None:
            col = self._find_col("scale_yuan", "规模")
            if col and col in df.columns:
                mask = df[col].isna() | (df[col] <= criteria.max_scale * 1e8)
                df = df[mask]
                filters_applied.append(f"规模≤{criteria.max_scale}亿")

        # 3. 收益率过滤（缺失数据不参与过滤，保留该基金）
        for period, key in [
            ("1m", "return_1m"),
            ("3m", "return_3m"),
            ("6m", "return_6m"),
            ("1y", "return_1y"),
        ]:
            threshold = getattr(criteria, f"min_return_{period}")
            if threshold is not None and threshold > 0:
                col = self._find_col(key)
                if col and col in df.columns:
                    mask = df[col].isna() | (df[col] >= threshold)
                    df = df[mask]
                    filters_applied.append(f"近{period.upper()}收益≥{threshold}%")

        # 4. 最大回撤过滤（缺失数据不参与过滤）
        if criteria.max_drawdown is not None:
            col = self._find_col("max_drawdown")
            if col and col in df.columns:
                mask = df[col].isna() | (df[col] <= criteria.max_drawdown)
                df = df[mask]
                filters_applied.append(f"最大回撤≤{criteria.max_drawdown*100}%")

        # 5. 夏普比率过滤（缺失数据不参与过滤）
        if criteria.min_sharpe is not None:
            col = self._find_col("sharpe_ratio")
            if col and col in df.columns:
                mask = df[col].isna() | (df[col] >= criteria.min_sharpe)
                df = df[mask]
                filters_applied.append(f"夏普比率≥{criteria.min_sharpe}")

        # 6. 排序
        sort_col = self._find_col(criteria.sort_by)
        if sort_col and sort_col in df.columns:
            df = df.sort_values(sort_col, ascending=criteria.sort_ascending)

        # 7. 分页
        df = df.iloc[criteria.skip : criteria.skip + criteria.top_n]

        logger.info(
            f"[Screener] {initial_count} → {len(df)} 只基金 "
            f"（过滤条件: {', '.join(filters_applied) if filters_applied else '无'}）"
        )
        return df

    def get_top_by_return(
        self,
        period: str = "1y",
        fund_type: Optional[str] = None,
        top_n: int = 20,
    ) -> pd.DataFrame:
        """快捷方法：按收益率排名"""
        period_map = {"1m": "return_1m", "3m": "return_3m", "6m": "return_6m", "1y": "return_1y"}
        criteria = ScreenCriteria(
            fund_types=[fund_type] if fund_type else [],
            sort_by=period_map.get(period, "return_1y"),
            top_n=top_n,
        )
        return self.screen(criteria)

    def get_low_drawdown_funds(
        self,
        max_drawdown: float = 0.15,
        min_return_1y: float = 5.0,
        top_n: int = 20,
    ) -> pd.DataFrame:
        """快捷方法：低回撤 + 正收益"""
        criteria = ScreenCriteria(
            max_drawdown=max_drawdown,
            min_return_1y=min_return_1y,
            sort_by="max_drawdown",
            sort_ascending=True,
            top_n=top_n,
        )
        return self.screen(criteria)

    def _find_col(self, *candidates: str) -> Optional[str]:
        """在 DataFrame 中查找第一个存在的列名"""
        for c in candidates:
            if c in self.df.columns:
                return c
        return None

    @staticmethod
    def calc_max_drawdown(nav_series: pd.Series) -> float:
        """
        从净值序列计算最大回撤

        Parameters
        ----------
        nav_series : pd.Series
            单位净值时间序列（按日期升序）

        Returns
        -------
        float : 最大回撤（小数，0.15 = 15%）
        """
        if len(nav_series) < 2:
            return 0.0
        cummax = nav_series.cummax()
        drawdown = (nav_series - cummax) / cummax
        return abs(float(drawdown.min()))

    @staticmethod
    def calc_sharpe_ratio(
        daily_returns: pd.Series,
        risk_free_rate: float = 0.03,
    ) -> float:
        """
        从日收益率序列计算年化夏普比率（近似）

        Parameters
        ----------
        daily_returns : pd.Series
            日收益率（%，如 0.5 表示 0.5%）
        risk_free_rate : float
            无风险利率（默认 3%）

        Returns
        -------
        float : 年化夏普比率
        """
        if len(daily_returns) < 20:
            return 0.0
        # 日收益率转为小数
        rets = daily_returns / 100.0
        excess = rets - (risk_free_rate / 252)
        mean = excess.mean()
        std = excess.std()
        if std == 0:
            return 0.0
        return float(np.sqrt(252) * mean / std)
