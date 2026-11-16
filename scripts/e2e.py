"""Exercise the submitted UI against a live server and capture demo evidence."""

import argparse
import json
from pathlib import Path
from time import perf_counter

from playwright.sync_api import expect, sync_playwright


def run(browser, url, output):
    size = {"width": 1280, "height": 800}
    context = browser.new_context(viewport=size, record_video_size=size, record_video_dir=str(output))
    page = context.new_page()
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    measurements = {}
    page.goto(url)
    page.get_by_role("button", name="Войти как стажёр").click()
    expect(page.get_by_role("heading", name="Помощник стажёра")).to_be_visible()
    page.screenshot(path=output / "home.png")
    page.wait_for_timeout(1400)
    page.get_by_role("button", name="Как получить доступ к вики?").click()
    source = page.get_by_role("button", name="Инструкция Alpha")
    expect(source).to_be_visible(timeout=22_000)
    page.screenshot(path=output / "chat.png")
    source.click()
    expect(page.locator(".source-text")).to_contain_text("Доступ в пространство команды Alpha")
    page.wait_for_timeout(2400)
    source.click()
    page.get_by_role("button", name="Подготовь заявку на доступ").click()
    page.get_by_label("Зачем нужен доступ?").fill("Нужны инструкции для первой задачи")
    page.wait_for_timeout(900)
    page.get_by_role("button", name="Создать черновик").click()
    link = page.get_by_role("link", name="Открыть заявку", exact=True)
    expect(link).to_be_visible()
    page.wait_for_timeout(1600)
    started = perf_counter()
    link.click()
    button = page.get_by_role("button", name="Отправить заявку")
    expect(button).to_be_visible()
    measurements["open_draft_ms"] = round((perf_counter() - started) * 1000)
    route = page.url
    page.screenshot(path=output / "draft.png")
    page.wait_for_timeout(3000)
    button.click()
    expect(page.get_by_role("heading", name="Заявка отправлена")).to_be_visible()
    request_id = page.locator(".request-number").inner_text()
    assert request_id.startswith("REQ-"), request_id
    page.screenshot(path=output / "result.png")
    page.wait_for_timeout(3800)
    page.reload()
    expect(page.get_by_role("heading", name="Заявка отправлена")).to_be_visible()
    expect(page.locator(".request-number")).to_have_text(request_id)
    page.get_by_role("link", name="К помощнику").click()
    page.get_by_role("button", name="Что с моей заявкой?").click()
    expect(page.get_by_text(request_id + " на рассмотрении.", exact=False)).to_be_visible(timeout=22_000)
    page.wait_for_timeout(3200)
    assert not errors, errors
    video = page.video
    context.close()
    video.save_as(output / "recording.webm")

    other = browser.new_context(viewport={"width": 375, "height": 800}, is_mobile=True, has_touch=True)
    page = other.new_page()
    page.goto(route)
    page.get_by_label("Логин").fill("demo-intern-3")
    page.get_by_label("Пароль").fill("alpha-demo-3")
    page.get_by_role("button", name="Войти как стажёр").click()
    expect(page.get_by_role("alert")).to_have_text("Черновик недоступен по этой ссылке.")
    assert page.evaluate("document.documentElement.scrollWidth === innerWidth"), "mobile overflow"
    page.get_by_role("link", name="К помощнику").click()
    page.get_by_role("button", name="Открыть форму черновика").click()
    page.get_by_label("Зачем нужен доступ?").fill("Проверка с телефона")
    page.get_by_role("button", name="Создать черновик").click()
    page.get_by_role("link", name="Открыть заявку", exact=True).click()
    page.get_by_role("button", name="Отправить заявку").click()
    expect(page.get_by_role("heading", name="Заявка отправлена")).to_be_visible()
    assert page.locator(".request-number").inner_text() != request_id
    assert page.evaluate("document.documentElement.scrollWidth === innerWidth"), "mobile result overflow"
    page.screenshot(path=output / "mobile-result.png")
    page.get_by_role("button", name="Выйти").click()
    page.get_by_label("Логин").fill("demo-intern-2")
    page.get_by_label("Пароль").fill("beta-demo-2")
    page.get_by_role("button", name="Войти как стажёр").click()
    expect(page.get_by_role("heading", name="Сценарий недоступен")).to_be_visible()
    other.close()
    measurements["request_id"] = request_id
    measurements["checks"] = ["login", "source", "draft", "confirm", "reload", "same status in chat", "same-team isolation", "cross-team isolation", "mobile submission", "mobile width"]
    (output / "e2e.json").write_text(json.dumps(measurements, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(measurements))


if __name__ == "__main__":
    cli = argparse.ArgumentParser()
    cli.add_argument("--url", default="http://127.0.0.1:8080")
    cli.add_argument("--output", type=Path, required=True)
    cli.add_argument("--browser")
    args = cli.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(executable_path=args.browser)
        try:
            run(browser, args.url, args.output)
        finally:
            browser.close()
