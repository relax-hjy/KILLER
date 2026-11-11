"""
数据获取层 — 多数据源适配
"""

from src.data.base import DataSource
from src.data.akshare_source import AkShareSource
from src.data.eastmoney_source import EastMoneySource

__all__ = ["DataSource", "AkShareSource", "EastMoneySource"]
