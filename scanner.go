package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ─── Встроенный JS-скрипт ────────────────────────────────────────────────────

// scannerScript — Playwright-скрипт для сбора метаданных страницы.
// Записывается во временный файл при каждом вызове runScanner.
const scannerScript = `
const { chromium } = require('playwright');

// Домены которые не интересны для тест-генерации (аналитика, CDN, реклама).
const SKIP_DOMAINS = [
  'google-analytics.com', 'googletagmanager.com', 'googlesyndication.com',
  'facebook.com', 'facebook.net', 'fbcdn.net',
  'hotjar.com', 'clarity.ms',
  'sentry.io', 'bugsnag.com',
  'doubleclick.net', 'adnxs.com',
  'intercom.io', 'intercomcdn.com',
  'cdn.', 'static.', 'assets.',
];

function shouldSkip(url) {
  return SKIP_DOMAINS.some(d => url.includes(d));
}

// Нормализует URL: убирает query-параметры выглядящие как ID (?token=..., ?_=...).
// Оставляет параметры которые описывают фильтрацию/пагинацию (page, limit, sort, etc.).
function normalizeUrl(rawUrl) {
  try {
    const u = new URL(rawUrl);
    const keep = ['page', 'limit', 'per_page', 'sort', 'order', 'filter', 'q', 'search', 'type', 'status', 'category'];
    const filtered = new URLSearchParams();
    for (const [k, v] of u.searchParams.entries()) {
      if (keep.includes(k.toLowerCase())) filtered.append(k, v);
    }
    const qs = filtered.toString();
    return u.origin + u.pathname + (qs ? '?' + qs : '');
  } catch (_) {
    return rawUrl;
  }
}

(async () => {
  const url = process.argv[2];
  if (!url) { console.error('Usage: node <script> <URL>'); process.exit(1); }

  let browser;
  try {
    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();

    // ── Перехват сети: подписываемся ДО goto ─────────────────────────────────
    const requestMap = new Map();

    page.on('request', req => {
      const type = req.resourceType();
      if (type !== 'xhr' && type !== 'fetch') return;
      const reqUrl = req.url();
      if (shouldSkip(reqUrl)) return;
      const key = req.method() + ':' + normalizeUrl(reqUrl);
      if (!requestMap.has(key)) {
        requestMap.set(key, {
          method:   req.method(),
          url:      reqUrl,
          path:     (() => { try { return new URL(reqUrl).pathname; } catch(_) { return reqUrl; } })(),
          type,
          status:   null,
          hasBody:  req.method() !== 'GET' && req.method() !== 'HEAD',
        });
      }
    });

    page.on('response', res => {
      const req = res.request();
      const type = req.resourceType();
      if (type !== 'xhr' && type !== 'fetch') return;
      const key = req.method() + ':' + normalizeUrl(req.url());
      if (requestMap.has(key)) {
        requestMap.get(key).status = res.status();
      }
    });

    // ── Навигация — ждём networkidle чтобы SPA успел сделать все запросы ────
    await page.goto(url, { waitUntil: 'networkidle', timeout: 30000 });

    // ── Сбор DOM-данных ───────────────────────────────────────────────────────
    const domResult = await page.evaluate(() => {

      // Проверяет видимость элемента в DOM (не hidden, не display:none, имеет размеры).
      function isVisible(el) {
        if (!el) return false;
        const style = window.getComputedStyle(el);
        if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
        const rect = el.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
      }

      // Возвращает текст лейбла для поля ввода (for="id", aria-labelledby, aria-label, placeholder).
      function getLabel(el) {
        const id = el.getAttribute('id');
        if (id) {
          const lbl = document.querySelector('label[for="' + id + '"]');
          if (lbl) return (lbl.textContent || '').trim().slice(0, 80);
        }
        const labelledBy = el.getAttribute('aria-labelledby');
        if (labelledBy) {
          const lbl = document.getElementById(labelledBy);
          if (lbl) return (lbl.textContent || '').trim().slice(0, 80);
        }
        return '';
      }

      // Считает сколько раз встречается данный CSS-селектор на странице.
      function countMatches(selector) {
        try { return document.querySelectorAll(selector).length; } catch(_) { return 1; }
      }

      // ── data-testid ──────────────────────────────────────────────────────────
      const testIdMap = new Map();
      document.querySelectorAll('[data-testid]').forEach(el => {
        const id = el.getAttribute('data-testid');
        if (!testIdMap.has(id)) {
          testIdMap.set(id, {
            testId:  id,
            tag:     el.tagName.toLowerCase(),
            type:    el.getAttribute('type') || '',
            text:    (el.textContent || '').trim().slice(0, 80),
            visible: isVisible(el),
            count:   0,
          });
        }
        testIdMap.get(id).count++;
      });
      const testIds = [...testIdMap.values()];

      // ── aria-label ───────────────────────────────────────────────────────────
      const ariaMap = new Map();
      document.querySelectorAll('[aria-label]').forEach(el => {
        const label = el.getAttribute('aria-label');
        const tag   = el.tagName.toLowerCase();
        const key   = label + '|' + tag;
        if (!ariaMap.has(key)) {
          ariaMap.set(key, {
            ariaLabel: label,
            tag,
            role:    el.getAttribute('role') || '',
            id:      el.getAttribute('id') || '',
            visible: isVisible(el),
            count:   0,
          });
        }
        ariaMap.get(key).count++;
      });
      const ariaLabels = [...ariaMap.values()];

      // ── Кнопки ───────────────────────────────────────────────────────────────
      const buttonMap = new Map();
      document.querySelectorAll(
        'button, [role="button"], input[type="submit"], input[type="button"]'
      ).forEach(el => {
        const text      = (el.textContent || el.getAttribute('value') || '').trim().slice(0, 80);
        const testId    = el.getAttribute('data-testid') || '';
        const ariaLabel = el.getAttribute('aria-label') || '';
        const name      = el.getAttribute('name') || '';
        const id        = el.getAttribute('id') || '';
        // Ключ дедупликации — по наиболее специфичному атрибуту
        const dedupeKey = testId || ariaLabel || name || id || text;
        if (!buttonMap.has(dedupeKey)) {
          buttonMap.set(dedupeKey, {
            tag:       el.tagName.toLowerCase(),
            type:      el.getAttribute('type') || '',
            text,
            testId,
            ariaLabel,
            name,
            id,
            visible:   isVisible(el),
            count:     0,
          });
        }
        buttonMap.get(dedupeKey).count++;
      });
      const buttons = [...buttonMap.values()];

      // ── Поля ввода ───────────────────────────────────────────────────────────
      const inputMap = new Map();
      document.querySelectorAll('input, textarea, select').forEach(el => {
        if (['hidden','submit','button'].includes(el.getAttribute('type'))) return;
        const name      = el.getAttribute('name') || '';
        const testId    = el.getAttribute('data-testid') || '';
        const ariaLabel = el.getAttribute('aria-label') || '';
        const id        = el.getAttribute('id') || '';
        const dedupeKey = testId || ariaLabel || name || id || (el.tagName + el.getAttribute('type'));
        if (!inputMap.has(dedupeKey)) {
          // Для select — собираем список опций
          let options = [];
          if (el.tagName.toLowerCase() === 'select') {
            options = [...el.querySelectorAll('option')]
              .map(o => ({ value: o.value, text: (o.textContent || '').trim() }))
              .slice(0, 20);
          }
          inputMap.set(dedupeKey, {
            tag:         el.tagName.toLowerCase(),
            type:        el.getAttribute('type') || '',
            name,
            placeholder: el.getAttribute('placeholder') || '',
            testId,
            ariaLabel,
            label:       getLabel(el),
            id,
            visible:     isVisible(el),
            count:       0,
            options,
          });
        }
        inputMap.get(dedupeKey).count++;
      });
      const inputs = [...inputMap.values()];

      // ── Ссылки ───────────────────────────────────────────────────────────────
      const links = [...document.querySelectorAll('a[href]')]
        .map(el => ({
          text:      (el.textContent || '').trim().slice(0, 80),
          href:      el.getAttribute('href'),
          testId:    el.getAttribute('data-testid') || '',
          ariaLabel: el.getAttribute('aria-label') || '',
          id:        el.getAttribute('id') || '',
          visible:   isVisible(el),
        }))
        .filter(l => l.text || l.ariaLabel)
        .slice(0, 30);

      // ── Заголовки ─────────────────────────────────────────────────────────────
      const headings = [...document.querySelectorAll('h1,h2,h3')].map(el => ({
        level: el.tagName.toLowerCase(),
        text:  (el.textContent || '').trim().slice(0, 120),
      }));

      return {
        title: document.title,
        url:   location.href,
        testIds,
        ariaLabels,
        buttons,
        inputs,
        links,
        headings,
      };
    });

    // ── Финальный результат: DOM + сетевые запросы ───────────────────────────
    const apiRequests = [...requestMap.values()].slice(0, 40);
    const result = Object.assign({}, domResult, { apiRequests });

    // Единственный вывод в stdout — чистый JSON
    process.stdout.write(JSON.stringify(result) + '\n');
  } catch (err) {
    console.error('Scanner error:', err.message);
    process.exit(1);
  } finally {
    if (browser) await browser.close();
  }
})();
`

// ─── Структуры результата ────────────────────────────────────────────────────

// ScanResult — полный результат сканирования страницы.
type ScanResult struct {
	Title       string          `json:"title"`
	URL         string          `json:"url"`
	TestIDs     []ScanTestID    `json:"testIds"`
	AriaLabels  []ScanAriaLabel `json:"ariaLabels"`
	Buttons     []ScanButton    `json:"buttons"`
	Inputs      []ScanInput     `json:"inputs"`
	Links       []ScanLink      `json:"links"`
	Headings    []ScanHeading   `json:"headings"`
	APIRequests []ScanRequest   `json:"apiRequests"`
}

// ScanRequest — один XHR/fetch-запрос перехваченный во время загрузки страницы.
type ScanRequest struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Path    string `json:"path"`
	Type    string `json:"type"`    // "xhr" или "fetch"
	Status  int    `json:"status"`  // HTTP-статус ответа, 0 если ответ не получен
	HasBody bool   `json:"hasBody"` // true для POST/PUT/PATCH — есть тело запроса
}

type ScanTestID struct {
	TestID  string `json:"testId"`
	Tag     string `json:"tag"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Visible bool   `json:"visible"`
	Count   int    `json:"count"` // сколько элементов с этим testId на странице
}

type ScanAriaLabel struct {
	AriaLabel string `json:"ariaLabel"`
	Tag       string `json:"tag"`
	Role      string `json:"role"`
	ID        string `json:"id"`
	Visible   bool   `json:"visible"`
	Count     int    `json:"count"`
}

type ScanButton struct {
	Tag       string `json:"tag"`
	Type      string `json:"type"`
	Text      string `json:"text"`
	TestID    string `json:"testId"`
	AriaLabel string `json:"ariaLabel"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	Visible   bool   `json:"visible"`
	Count     int    `json:"count"` // >1 → strict mode violation без .first()
}

// ScanSelectOption — одна опция <select>.
type ScanSelectOption struct {
	Value string `json:"value"`
	Text  string `json:"text"`
}

type ScanInput struct {
	Tag         string             `json:"tag"`
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Placeholder string             `json:"placeholder"`
	TestID      string             `json:"testId"`
	AriaLabel   string             `json:"ariaLabel"`
	Label       string             `json:"label"`
	ID          string             `json:"id"`
	Visible     bool               `json:"visible"`
	Count       int                `json:"count"`
	Options     []ScanSelectOption `json:"options,omitempty"` // только для <select>
}

type ScanLink struct {
	Text      string `json:"text"`
	Href      string `json:"href"`
	TestID    string `json:"testId"`
	AriaLabel string `json:"ariaLabel"`
	ID        string `json:"id"`
	Visible   bool   `json:"visible"`
}

type ScanHeading struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// ─── Запуск сканера ──────────────────────────────────────────────────────────

// runScanner записывает встроенный JS во временный файл, запускает через node,
// извлекает JSON из stdout и возвращает ScanResult.
// Временный файл удаляется автоматически через defer.
func runScanner(targetURL string) (*ScanResult, error) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("node не найден в PATH\n[TIP] Установите Node.js: https://nodejs.org")
	}

	// Рабочая директория — там где лежит node_modules (CWD при go run и обычном запуске).
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("не удалось определить рабочую директорию: %v", err)
	}

	// #5 исправлено: вычисляем nodeModules один раз, передаём в обе функции.
	nodeModules := filepath.Join(cwd, "node_modules")

	if err := checkPlaywright(nodePath, cwd, nodeModules); err != nil {
		return nil, err
	}

	// Пишем скрипт во временный файл рядом с node_modules —
	// так Node.js корректно резолвит require('playwright').
	tmp, err := os.CreateTemp(cwd, ".conveyor-scanner-*.js")
	if err != nil {
		return nil, fmt.Errorf("не удалось создать временный файл: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(scannerScript); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("ошибка записи скрипта: %v", err)
	}
	tmp.Close()

	fmt.Printf("\033[36m[SCAN]\033[0m Сканирую %s ...\n", targetURL)

	env := append(os.Environ(), "NODE_PATH="+nodeModules)
	cmd := exec.Command(nodePath, tmp.Name(), targetURL)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf(
			"ошибка выполнения сканера: %v\n[TIP] npm install playwright && npx playwright install chromium",
			err,
		)
	}

	// #3 исправлено: ищем строку с JSON среди возможных предупреждений Node/Playwright.
	// Скрипт пишет JSON через process.stdout.write — это последняя непустая строка.
	jsonLine := extractJSONLine(output)
	if jsonLine == nil {
		return nil, fmt.Errorf("не удалось найти JSON в выводе сканера:\n%s", string(output))
	}

	var result ScanResult
	if err := json.Unmarshal(jsonLine, &result); err != nil {
		return nil, fmt.Errorf("ошибка разбора результата: %v", err)
	}

	fmt.Printf("\033[32m[SCAN OK]\033[0m %q\n", result.Title)
	fmt.Printf("          testid:%d  aria:%d  buttons:%d  inputs:%d  links:%d  api:%d\n",
		len(result.TestIDs), len(result.AriaLabels),
		len(result.Buttons), len(result.Inputs), len(result.Links), len(result.APIRequests))

	return &result, nil
}

// extractJSONLine находит первую строку в выводе которая является валидным JSON-объектом.
// Это защищает от предупреждений Node.js / Playwright которые могут попасть в stdout.
func extractJSONLine(output []byte) []byte {
	for _, line := range bytes.Split(output, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) > 0 && line[0] == '{' {
			return line
		}
	}
	return nil
}

// checkPlaywright проверяет доступность модуля playwright через node.
// #5 исправлено: принимает nodeModules снаружи — не вызывает os.Environ() повторно.
func checkPlaywright(nodePath, cwd, nodeModules string) error {
	cmd := exec.Command(nodePath, "-e", "require('playwright')")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "NODE_PATH="+nodeModules)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"playwright не найден\n[TIP] Установите в папке с conveyor:\n" +
				"      npm install playwright && npx playwright install chromium",
		)
	}
	return nil
}

// ─── Форматирование контекста ────────────────────────────────────────────────

// buttonLocator возвращает наилучший Playwright-локатор для кнопки.
// Если на странице несколько элементов с одинаковым селектором — добавляет .first().
func buttonLocator(b ScanButton) string {
	suffix := ""
	if b.Count > 1 {
		suffix = ".first() /* ⚠ на странице " + fmt.Sprintf("%d", b.Count) + " таких элемента — без .first() будет strict mode violation */"
	}
	switch {
	case b.TestID != "":
		return fmt.Sprintf("page.getByTestId(%q)%s", b.TestID, suffix)
	case b.AriaLabel != "":
		role := inferRole(b.Tag, b.Type)
		return fmt.Sprintf("page.getByRole(%q, { name: %q })%s", role, b.AriaLabel, suffix)
	case b.Text != "":
		role := inferRole(b.Tag, b.Type)
		return fmt.Sprintf("page.getByRole(%q, { name: %q })%s", role, b.Text, suffix)
	case b.Name != "":
		return fmt.Sprintf("page.locator('[name=%q]')%s", b.Name, suffix)
	case b.ID != "":
		return fmt.Sprintf("page.locator('#%s')%s", b.ID, suffix)
	default:
		return fmt.Sprintf("page.locator('%s[type=%q]')%s", b.Tag, b.Type, suffix)
	}
}

// inputLocator возвращает наилучший Playwright-локатор для поля ввода.
func inputLocator(inp ScanInput) string {
	suffix := ""
	if inp.Count > 1 {
		suffix = ".first() /* ⚠ на странице " + fmt.Sprintf("%d", inp.Count) + " таких элемента */"
	}
	switch {
	case inp.TestID != "":
		return fmt.Sprintf("page.getByTestId(%q)%s", inp.TestID, suffix)
	case inp.Label != "":
		return fmt.Sprintf("page.getByLabel(%q)%s", inp.Label, suffix)
	case inp.AriaLabel != "":
		return fmt.Sprintf("page.getByLabel(%q)%s", inp.AriaLabel, suffix)
	case inp.Placeholder != "":
		return fmt.Sprintf("page.getByPlaceholder(%q)%s", inp.Placeholder, suffix)
	case inp.Name != "":
		return fmt.Sprintf("page.locator('[name=%q]')%s", inp.Name, suffix)
	case inp.ID != "":
		return fmt.Sprintf("page.locator('#%s')%s", inp.ID, suffix)
	default:
		if inp.Tag == "textarea" {
			return "page.locator('textarea')" + suffix
		}
		return fmt.Sprintf("page.locator('input[type=%q]')%s", inp.Type, suffix)
	}
}

// inferRole определяет ARIA-роль по тегу и типу элемента.
func inferRole(tag, typ string) string {
	switch tag {
	case "button":
		return "button"
	case "input":
		switch typ {
		case "submit", "button":
			return "button"
		case "checkbox":
			return "checkbox"
		case "radio":
			return "radio"
		}
	case "a":
		return "link"
	case "select":
		return "combobox"
	}
	return "button"
}

// formatScanContext превращает ScanResult в читаемый текстовый контекст для LLM.
// Выдаёт готовые Playwright-локаторы (copy-paste ready), предупреждает о дубликатах.
func formatScanContext(scan *ScanResult) string {
	var sb strings.Builder

	sb.WriteString("=== Автосканирование страницы ===\n")
	sb.WriteString(fmt.Sprintf("URL:   %s\n", scan.URL))
	sb.WriteString(fmt.Sprintf("Title: %s\n", scan.Title))

	// ── Заголовки ──────────────────────────────────────────────────────────────
	if len(scan.Headings) > 0 {
		sb.WriteString("\n--- Заголовки ---\n")
		for _, h := range scan.Headings {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", h.Level, h.Text))
		}
	}

	// ── Кнопки с готовыми локаторами ───────────────────────────────────────────
	if len(scan.Buttons) > 0 {
		sb.WriteString("\n--- Кнопки (готовые локаторы) ---\n")
		for _, b := range scan.Buttons {
			vis := ""
			if !b.Visible {
				vis = " [скрыта]"
			}
			sb.WriteString(fmt.Sprintf("  %s%s\n", buttonLocator(b), vis))
		}
	}

	// ── Поля ввода с готовыми локаторами ───────────────────────────────────────
	if len(scan.Inputs) > 0 {
		sb.WriteString("\n--- Поля ввода (готовые локаторы) ---\n")
		for _, inp := range scan.Inputs {
			vis := ""
			if !inp.Visible {
				vis = " [скрыто]"
			}
			loc := inputLocator(inp)
			// Для select показываем доступные опции
			if inp.Tag == "select" && len(inp.Options) > 0 {
				var opts []string
				for _, o := range inp.Options {
					if o.Text != "" {
						opts = append(opts, fmt.Sprintf("%q", o.Text))
					}
				}
				sb.WriteString(fmt.Sprintf("  %s%s\n", loc, vis))
				sb.WriteString(fmt.Sprintf("    → page.selectOption(%s, { label: %s })\n",
					loc, strings.Join(opts[:min(3, len(opts))], " | ")))
			} else {
				sb.WriteString(fmt.Sprintf("  %s%s\n", loc, vis))
			}
		}
	}

	// ── data-testid (полный список) ────────────────────────────────────────────
	if len(scan.TestIDs) > 0 {
		sb.WriteString("\n--- data-testid на странице ---\n")
		for _, t := range scan.TestIDs {
			line := fmt.Sprintf("  page.getByTestId(%q)", t.TestID)
			if t.Text != "" {
				line += fmt.Sprintf("  // %s", t.Text)
			}
			if t.Count > 1 {
				line += fmt.Sprintf("  ⚠ ×%d", t.Count)
			}
			if !t.Visible {
				line += "  [скрыт]"
			}
			sb.WriteString(line + "\n")
		}
	}

	// ── aria-label (для getByRole) ─────────────────────────────────────────────
	if len(scan.AriaLabels) > 0 {
		sb.WriteString("\n--- aria-label (для getByRole/getByLabel) ---\n")
		for _, a := range scan.AriaLabels {
			role := a.Role
			if role == "" {
				role = inferRole(a.Tag, "")
			}
			line := fmt.Sprintf("  page.getByRole(%q, { name: %q })", role, a.AriaLabel)
			if a.Count > 1 {
				line += fmt.Sprintf("  ⚠ ×%d → добавь .first()", a.Count)
			}
			if !a.Visible {
				line += "  [скрыт]"
			}
			sb.WriteString(line + "\n")
		}
	}

	// ── Ссылки ─────────────────────────────────────────────────────────────────
	if len(scan.Links) > 0 {
		sb.WriteString("\n--- Ссылки ---\n")
		for _, l := range scan.Links {
			if !l.Visible {
				continue // скрытые ссылки не интересны
			}
			var loc string
			switch {
			case l.TestID != "":
				loc = fmt.Sprintf("page.getByTestId(%q)", l.TestID)
			case l.AriaLabel != "":
				loc = fmt.Sprintf("page.getByRole(\"link\", { name: %q })", l.AriaLabel)
			case l.Text != "":
				loc = fmt.Sprintf("page.getByRole(\"link\", { name: %q })", l.Text)
			default:
				loc = fmt.Sprintf("page.locator('a[href=%q]')", l.Href)
			}
			sb.WriteString(fmt.Sprintf("  %s  // href=%s\n", loc, l.Href))
		}
	}

	// ── API-запросы ────────────────────────────────────────────────────────────
	if len(scan.APIRequests) > 0 {
		sb.WriteString("\n--- API запросы (XHR/fetch) ---\n")
		for _, r := range scan.APIRequests {
			statusStr := "pending"
			if r.Status > 0 {
				statusStr = fmt.Sprintf("%d", r.Status)
			}
			displayPath := r.Path
			if len(displayPath) > 80 {
				displayPath = displayPath[:77] + "..."
			}
			line := fmt.Sprintf("  %-6s %-4s %s", r.Method, statusStr, displayPath)
			if r.HasBody {
				line += "  [body]"
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n  Hint: page.waitForResponse(resp => resp.url().includes('/path')) — ")
		sb.WriteString("оборачивай вместе с триггером в Promise.all([])\n")
	}

	return sb.String()
}

