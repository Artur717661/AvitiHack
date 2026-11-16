"""Merge app line coverage; derive Go line hits from the official block profile."""

import json
import re
from pathlib import Path


root = Path(__file__).resolve().parents[1]
go_profile = root / "backend.cover"
web_profile = root / "web/coverage/coverage-summary.json"
python_profile = root / "coverage.json"
output = root / "reports/coverage"


def go_coverage():
    source = {}
    lines = {}
    statements = [0, 0]
    for block in go_profile.read_text(encoding="utf-8").splitlines()[1:]:
        match = re.fullmatch(r"(.+?):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)", block)
        if not match:
            raise ValueError(f"Invalid Go coverage block: {block}")
        path, first, last, count, hits = match.groups()
        path = path.removeprefix("github.com/Artur717661/AvitiHack/")
        if int(count) == 0:
            continue
        if path not in source:
            source[path] = (root / path).read_text(encoding="utf-8").splitlines()
        statements[0] += int(count) if int(hits) else 0
        statements[1] += int(count)
        for number in range(int(first), int(last) + 1):
            code = source[path][number - 1].strip()
            if not code or code.startswith("//") or code in {"}", ")", "},"}:
                continue
            key = (path, number)
            lines[key] = lines.get(key, False) or int(hits) > 0
    return (sum(lines.values()), len(lines)), statements


def raw_summary():
    go, statements = go_coverage()
    web = json.loads(web_profile.read_text(encoding="utf-8"))["total"]["lines"]
    python = json.loads(python_profile.read_text(encoding="utf-8"))["totals"]
    return {
        "Go · строки из профиля блоков": go,
        "React/TypeScript · Vitest/V8": (web["covered"], web["total"]),
        "Python ML · coverage.py": (python["covered_lines"], python["num_statements"]),
    }, statements


if __name__ == "__main__":
    rows, statements = raw_summary()
    covered, total = map(sum, zip(*rows.values()))
    rows["Всего"] = (covered, total)
    summary = [
        "# Покрытие прикладного кода",
        "",
        "| Область | Покрыто | Всего | Покрытие |",
        "|---|---:|---:|---:|",
    ]
    for label, (hits, count) in rows.items():
        summary.append(f"| {label} | {hits} | {count} | {100 * hits/count:.2f}% |")
    summary += [
        "",
        f"Нативное покрытие операторов Go: {statements[0]}/{statements[1]} ({100 * statements[0]/statements[1]:.2f}%).",
        "",
        "Go учитывает непустые строки из исполняемых блоков, кроме комментариев и закрывающих скобок. Пересекающиеся блоки объединены. Это приближение по профилю go test. Vitest и coverage.py используют свои счётчики. Итог взвешен по числу строк. Тесты, зависимости, сборка фронта, инструменты скачивания и отчётности не входят в прикладной код. В тестах Python проверяется HTTP-обёртка и контракт модели. Загрузка и генерация требуют отдельных весов.",
        "",
        "Команды повторения - в README. Исходные машинные отчёты лежат рядом.",
        "",
    ]
    output.mkdir(parents=True, exist_ok=True)
    (output / "summary.md").write_text("\n".join(summary), encoding="utf-8")
    (output / "backend.cover").write_bytes(go_profile.read_bytes())
    (output / "web-summary.json").write_text(json.dumps(json.loads(web_profile.read_text(encoding="utf-8"))["total"], indent=2), encoding="utf-8")
    (output / "ml-coverage.json").write_bytes(python_profile.read_bytes())
    print(f"{covered}/{total} lines: {100 * covered/total:.2f}%")
    if covered * 100 <= total * 80:
        raise SystemExit("Coverage must be above 80%")
