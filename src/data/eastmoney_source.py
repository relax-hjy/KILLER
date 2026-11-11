"""
天天基金网直连数据源（AkShare 的 Fallback）

当 AkShare 因网页结构变更等原因失效时，
回退到此模块直接从天天基金网 HTTP 接口抓取数据。

接口参考：天天基金网公开 API（无认证要求）
"""

import json
import re
from datetime import datetime
from typing import Optional

import pandas as pd
import requests
from loguru import logger

from src.data.base import DataSource


# --- 天天基金网 HTTP API 常量 ---

FUND_LIST_URL = "http://fund.eastmoney.com/js/fundcode_search.js"
FUND_NAV_URL = (
    "http://fund.eastmoney.com/f10/F10DataApi.aspx"
    "?type=lsjz&code={code}&page={page}&sdate={start}&edate={end}&per={per}"
)
FUND_ESTIMATE_URL = "http://fundgz.1234567.com.cn/js/{code}.js"
FUND_DETAIL_URL = "http://fund.eastmoney.com/pingzhongdata/{code}.js"

HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
        "AppleWebKit/537.36 (KHTML, like Gecko) "
        "Chrome/125.0.0.0 Safari/537.36"
    ),
    "Referer": "http://fund.eastmoney.com/",
}


class EastMoneySource(DataSource):
    """天天基金网直连 — Fallback 数据源"""

    name = "eastmoney"

    def fetch_fund_list(self) -> pd.DataFrame:
        """
        从天天基金网 JS 文件中解析基金列表

        返回格式: [[code, pinyin, name, type, pinyin_full], ...]
        """
        logger.info("[EastMoney] 拉取基金列表 ...")
        try:
            resp = requests.get(FUND_LIST_URL, headers=HEADERS, timeout=15)
            resp.raise_for_status()
            resp.encoding = "utf-8"

            # 提取 var r = [ ... ];
            match = re.search(r"var\s+r\s*=\s*(\[\[.*?\]\]);", resp.text, re.DOTALL)
            if not match:
                raise ValueError("无法从返回内容中解析基金列表 JSON")

            raw = json.loads(match.group(1))
            df = pd.DataFrame(
                raw,
                columns=["code", "pinyin", "name", "fund_type", "pinyin_full"],
            )
            logger.success(f"[EastMoney] 获取到 {len(df)} 只基金")
            return df

        except Exception as e:
            logger.error(f"[EastMoney] 基金列表拉取失败: {e}")
            raise

    def fetch_nav_history(
        self,
        fund_code: str,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> pd.DataFrame:
        """
        从天天基金网 F10DataApi 接口逐页拉取净值历史
        """
        logger.info(f"[EastMoney] 拉取基金 {fund_code} 净值历史 ...")

        sd = start_date or "2010-01-01"
        ed = end_date or datetime.now().strftime("%Y-%m-%d")

        all_pages: list[pd.DataFrame] = []
        page = 1

        try:
            while True:
                url = FUND_NAV_URL.format(
                    code=fund_code, page=page, start=sd, end=ed, per=40
                )
                resp = requests.get(url, headers=HEADERS, timeout=15)
                resp.encoding = "utf-8"

                # 响应格式: var apidata={ content:"...", ... }
                html_match = re.search(r"content:\"(.*?)\",", resp.text)
                if not html_match:
                    break

                html_content = html_match.group(1)
                # 解码 HTML 实体
                html_content = html_content.replace("\\\"", "\"")

                tables = pd.read_html(html_content)
                if not tables or tables[0].empty:
                    break

                df_page = tables[0]
                all_pages.append(df_page)

                # 检查是否有下一页（页面数据不足40条则到最后一页）
                if len(df_page) < 40:
                    break

                page += 1

            if not all_pages:
                logger.warning(f"[EastMoney] 基金 {fund_code} 无净值数据")
                return pd.DataFrame()

            df = pd.concat(all_pages, ignore_index=True)
            logger.success(f"[EastMoney] 基金 {fund_code}: {len(df)} 条净值记录")
            return df

        except Exception as e:
            logger.error(f"[EastMoney] 基金 {fund_code} 净值拉取失败: {e}")
            raise

    def fetch_realtime_estimate(self) -> pd.DataFrame:
        """
        拉取基金实时估值

        注意：天天基金网的实时估值接口按单只基金提供，
        全量拉取开销较大，这里返回空 DataFrame，
        建议业务层针对自选基金逐只调用 fetch_single_estimate()。
        """
        logger.warning("[EastMoney] 全量实时估值不支持，请使用单只查询")
        return pd.DataFrame()

    def fetch_single_estimate(self, fund_code: str) -> dict:
        """
        单只基金实时估值

        Returns
        -------
        dict with keys:
            fundcode, name, jzrq, dwjz, gsz, gszzl, gztime
            （基金代码/名称/净值日期/单位净值/估算值/估算涨跌幅/估值时间）
        """
        url = FUND_ESTIMATE_URL.format(code=fund_code)
        try:
            resp = requests.get(url, headers=HEADERS, timeout=10)
            resp.encoding = "utf-8"
            # 响应: jsonpgz({...});
            match = re.search(r"jsonpgz\((\{.*?\})\);", resp.text, re.DOTALL)
            if match:
                return json.loads(match.group(1))
            return {}
        except Exception as e:
            logger.error(f"[EastMoney] 基金 {fund_code} 估值拉取失败: {e}")
            return {}

    def fetch_portfolio(self, fund_code: str, report_date: str) -> pd.DataFrame:
        """
        基金持仓数据需要从 fund.eastmoney.com 页面解析，
        这里返回空 DataFrame 作为降级策略。
        完整实现可参考 AkShare 源码。
        """
        logger.warning(f"[EastMoney] 持仓数据暂不支持直连，请切换到 AkShare")
        return pd.DataFrame()
