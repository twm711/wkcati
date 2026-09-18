# -*- coding: utf-8 -*-
"""
UI 冒烟测试（Playwright）：直接驱动工作区的 3 份 HTML 交互原型（file:// 协议）。
对应测试用例集：M04 组（设计器）、M07 组（坐席端）、M08 组（监控墙）。

运行前置：
  pip install pytest playwright
  playwright install chromium
未安装 playwright 时自动跳过（不影响纯逻辑用例执行）。
"""
import os
import pathlib

import pytest

pw = pytest.importorskip("playwright.sync_api", reason="playwright 未安装，跳过 UI 冒烟用例")

ROOT = pathlib.Path(__file__).resolve().parent.parent
DESIGNER = ROOT / "ITACATI_问卷设计器_交互原型.html"
AGENT = ROOT / "ITACATI_坐席端_外呼作答原型.html"
SUPERVISOR = ROOT / "ITACATI_督导端_监控墙原型.html"
IVR = ROOT / "ITACATI_IVR设计器_交互原型.html"


@pytest.fixture(scope="session")
def page():
    from playwright.sync_api import sync_playwright
    with sync_playwright() as p:
        browser = p.chromium.launch()
        pg = browser.new_page()
        yield pg
        browser.close()


def _open(page, path):
    page.goto(path.resolve().as_uri())
    page.wait_for_timeout(400)


# ── M04 问卷设计器（TC-M04-001/006/012） ──
class TestDesignerPrototype:
    def test_load_and_seed_questions(self, page):
        """P0：原型加载后有 6 道种子题，画布渲染正常"""
        _open(page, DESIGNER)
        assert page.locator(".qcard").count() >= 6
        assert "客户满意度调查_2026" in page.locator("#svyTitle").input_value()

    def test_add_question_via_palette(self, page):
        """P1：点击左侧题型库追加题目，题数 +1"""
        _open(page, DESIGNER)
        before = page.locator(".qcard").count()
        page.locator(".qt-item").first.click()
        page.wait_for_timeout(200)
        assert page.locator(".qcard").count() == before + 1

    def test_validate_report_opens(self, page):
        """P1：逻辑校验弹窗可打开（含通过或问题项）"""
        _open(page, DESIGNER)
        page.get_by_role("button", name="✓ 逻辑校验").click()
        page.wait_for_timeout(300)
        assert page.locator(".issue").count() >= 1

    def test_preview_modal_and_must_answer(self, page):
        """P0（TC-M04-003）：预览中必答题不答被拦截"""
        _open(page, DESIGNER)
        page.get_by_role("button", name="▶ 预览（模拟坐席作答）").click()
        page.wait_for_timeout(300)
        page.get_by_role("button", name="下一题 →").click()   # 甄别题必答直接下一题
        page.wait_for_timeout(200)
        assert "必答" in page.locator("#pvErr").text_content()

    def test_dead_question_detection(self, page):
        """P1（TC-M04-006）：构造不可达题后校验给出死题警告
        步骤：删空问卷 → 加一题并让 Q1 跳 END 前先加 2 题（不可达）→ 校验
        """
        _open(page, DESIGNER)
        page.evaluate("survey.questions = []; render();")
        page.evaluate("""
          addQuestion('single');
          survey.questions[0].options[0].jump='end'; render();
          addQuestion('single'); addQuestion('single'); render();
        """)
        page.get_by_role("button", name="✓ 逻辑校验").click()
        page.wait_for_timeout(300)
        text = page.locator("#modalRoot").text_content()
        assert "无法被访问" in text


# ── M07 坐席端（TC-M07-001/004/008） ──
class TestAgentPrototype:
    def test_login_and_ready(self, page):
        """P0（TC-M07-001）：签入后状态徽章变为就绪"""
        _open(page, AGENT)
        page.get_by_role("button", name="签 入").click()
        page.wait_for_timeout(400)
        assert "就绪" in page.locator("#stateBadge").text_content()

    def test_sample_assigned_after_login(self, page):
        """P0（派样）：签入后左栏出现样本信息（派样锁演示）"""
        _open(page, AGENT)
        page.get_by_role("button", name="签 入").click()
        page.wait_for_timeout(600)
        assert "S-10" in page.locator("#samplePanel").text_content()

    def test_dial_and_reach_terminal(self, page):
        """P0（外呼模拟）：呼叫后 15s 内进入 通话/后处理/就绪 之一（随机话务结果）"""
        _open(page, AGENT)
        page.get_by_role("button", name="签 入").click()
        page.wait_for_timeout(500)
        page.locator("#btnDial").click()
        page.wait_for_timeout(15000)
        state = page.locator("#stateBadge").text_content()
        assert any(k in state for k in ("通话", "后处理", "就绪"))

    def test_result_code_disabled_before_call(self, page):
        """P1：未接通时结果码按钮禁用（防误提交）"""
        _open(page, AGENT)
        assert page.locator("#rc-SUCCESS").is_disabled()


# ── M08 督导监控墙（TC-M08-002/004/005） ──
class TestSupervisorPrototype:
    def test_wall_renders_agents(self, page):
        """P0：监控墙渲染 12 个坐席卡"""
        _open(page, SUPERVISOR)
        assert page.locator(".acard").count() == 12

    def test_listen_flow(self, page):
        """P0（TC-M08-002）：对通话中坐席发起监听 → 弹出监听窗，状态栏更新"""
        _open(page, SUPERVISOR)
        page.locator(".acard.TALKING").first.locator("button:has-text('🎧监听')").click()
        page.wait_for_timeout(300)
        assert "监听中" in page.locator(".mhead").text_content()
        assert page.locator("#sbListen").text_content() != "无"

    def test_force_busy_writes_event(self, page):
        """P1（TC-M08-003）：强制示忙后坐席变示忙且事件日志留痕"""
        _open(page, SUPERVISOR)
        card = page.locator(".acard.READY").first
        no = card.locator(".a-no").text_content()
        card.locator("button:has-text('🚫强忙')").click()
        page.on("dialog", lambda d: d.accept())
        page.wait_for_timeout(300)
        page.locator(".modal .mfoot .btn.danger, .modal .mfoot .btn.primary").first.click()
        page.wait_for_timeout(400)
        row = page.locator(f"#card-{no}")
        assert "示忙" in row.locator(".a-state").text_content()
        assert "FORCEBUSY" in page.locator("#elog").text_content()

    def test_quota_bar_shows(self, page):
        """P1（TC-M08-005）：配额进度条渲染 5 组"""
        _open(page, SUPERVISOR)
        assert page.locator(".q-row").count() == 5


# ── M14 IVR 设计器（TC-M14-001/004） ──
class TestIVRPrototype:
    def test_flow_tree_renders(self, page):
        """P0：流程树渲染 6 个种子节点"""
        _open(page, IVR)
        assert page.locator(".ncard").count() == 6

    def test_call_simulator_menu_branch(self, page):
        """P0（TC-M14-001）：来电模拟器按 2 进入查询分支"""
        _open(page, IVR)
        page.get_by_role("button", name="▶ 来电模拟器").click()
        page.wait_for_timeout(300)
        page.locator(".sim-keys button", has_text="2").click()   # 办证查询
        page.wait_for_timeout(300)
        assert "查询接口" in page.locator(".sim-lcd").text_content()

    def test_validate_ok(self, page):
        """P1：种子流程校验通过"""
        _open(page, IVR)
        page.get_by_role("button", name="✓ 逻辑校验").click()
        page.wait_for_timeout(300)
        assert "校验通过" in page.locator("#modalRoot").text_content()
