# -*- coding: utf-8 -*-
"""公共 fixture：原型文件路径、基础 URL 等"""
import pathlib

import pytest

ROOT = pathlib.Path(__file__).resolve().parent.parent

PROTOTYPES = {
    "designer": ROOT / "ITACATI_问卷设计器_交互原型.html",
    "agent": ROOT / "ITACATI_坐席端_外呼作答原型.html",
    "supervisor": ROOT / "ITACATI_督导端_监控墙原型.html",
    "ivr": ROOT / "ITACATI_IVR设计器_交互原型.html",
}


@pytest.fixture(scope="session")
def proto():
    return PROTOTYPES


@pytest.fixture(scope="session")
def app_base_url():
    """真实部署地址（可选）：设置环境变量 APP_BASE_URL 后 UI 用例可指向真实系统"""
    import os
    return os.environ.get("APP_BASE_URL", "")
