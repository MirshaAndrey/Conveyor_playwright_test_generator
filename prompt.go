package main

import (
	"fmt"
	"strings"
)

// ─── Системные промпты ────────────────────────────────────────────────────────

// defaultSystemPrompt — используется если system_prompt не задан в config.json.
const defaultSystemPrompt = `Ты — senior QA Automation инженер. Генерируй production-ready Playwright тесты.
Требования: TypeScript, Playwright Test, test.describe + test, переиспользуемый код.
Селекторы: используй getByTestId (предпочтительно), getByRole (с name), getByLabel; избегай XPath, nth-child и нестабильных селекторов.
Логика: каждый шаг чеклиста → действие + проверка; после каждого действия добавляй expect; проверяй UI состояние (visible, enabled, text).
Умные допущения: если нет селекторов — генерируй логичные data-testid (login-button, submit-form) и имитируй реальный пользовательский сценарий.
Анти-паттерны (запрещено): waitForTimeout, console.log, пустые тесты, TODO, невалидный код.
Ответ — только TypeScript код без пояснений и без markdown-блоков.`

// reviewSystemPrompt — второй проход: детальное ревью с конкретными правилами замены.
const reviewSystemPrompt = `Ты — senior QA Automation Engineer. Получишь задание и сгенерированный Playwright тест.
Найди все проблемы и верни ИСПРАВЛЕННЫЙ код. Ответ — только TypeScript без пояснений и без markdown-блоков.

═══ БЛОК 1: ПОКРЫТИЕ ЗАДАНИЯ ═══
• Сверь каждый шаг задания с тестом. Если шаг не покрыт — добавь действие + expect.
• Каждое действие пользователя (клик, ввод, переход) обязано иметь expect ПОСЛЕ него.
• Недостаточно: await page.click('...')
  Правильно:   await page.click('...'); await expect(page.locator('...')).toBeVisible();

═══ БЛОК 2: ЗАПРЕЩЁННЫЕ ПАТТЕРНЫ → ЗАМЕНЫ ═══
• waitForTimeout(N)          → waitFor({state:'visible'}) или waitForLoadState('networkidle')
• .click() без expect        → .click() + expect на изменение UI
• expect(true).toBe(true)    → удалить, заменить реальной проверкой состояния
• expect(false).toBe(false)  → удалить, заменить реальной проверкой состояния
• console.log(...)           → удалить полностью
• any / TODO / FIXME         → устранить: вывести конкретный тип или реализовать логику
• Пустые test('...', () => {}) → реализовать или удалить
• page.waitForNavigation()   → Promise.all([page.waitForURL(...), element.click()])
• fill() сразу перед waitFor кнопки → сначала blur() чтобы закрыть автокомплит:
    ❌ await input.fill(text); await btn.waitFor({state:'visible'});
    ✅ await input.fill(text); await input.blur(); await btn.waitFor({state:'visible'});
• new RegExp с URL содержащим ? → predicate-функция или экранирование \\?:
    ❌ page.waitForURL(new RegExp('/search?q=term'))
    ✅ page.waitForURL(url => url.includes('/search') && url.includes('q=term'))
    ✅ page.waitForURL(new RegExp('/search\\?q='))

═══ БЛОК 3: СЕЛЕКТОРЫ ═══
• XPath (//div, //*[@id])    → getByTestId('...') или getByRole('button', {name:'...'})
• nth-child / nth-of-type    → getByTestId с уточняющим атрибутом
• CSS-классы (.btn-primary)  → getByRole или getByTestId
• Порядок предпочтения: getByTestId > getByRole({name}) > getByLabel > getByText (точный текст)

═══ БЛОК 4: СТРУКТУРА И КАЧЕСТВО ═══
• Все типы должны быть явными — нет неявного any, нет необъявленных переменных.
• beforeEach должен содержать только навигацию и общие предусловия — не бизнес-логику.
• Каждый test проверяет ровно один сценарий — не смешивать позитивный и негативный путь.
• Названия тестов описывают ожидаемое поведение: 'должен показать ошибку при пустом email'.

═══ ПРАВИЛО ВЫВОДА ═══
Если нашёл хотя бы одну проблему из блоков выше — исправь и верни полный файл.
Если код полностью корректен — верни его без изменений.
Никаких комментариев, объяснений, markdown-блоков — только TypeScript код.`

// ─── Подстановка шаблона ─────────────────────────────────────────────────────

// applySystemPromptTemplate заменяет {{context}} в шаблоне промпта.
// {{input}} убирается — задача уходит отдельным пользовательским сообщением.
func applySystemPromptTemplate(tpl, context string) string {
	result := strings.ReplaceAll(tpl, "{{context}}", context)
	result = strings.ReplaceAll(result, "{{input}}", "")
	return strings.TrimSpace(result)
}

// ─── Формирование пользовательского промпта ──────────────────────────────────

// buildCasePrompt формирует структурированный запрос из TestCase.
// Контекст приложения уже встроен в системный промпт через applySystemPromptTemplate,
// поэтому здесь используется только prompt_hint конкретного кейса.
func buildCasePrompt(tc TestCase) string {
	var sb strings.Builder
	sb.WriteString("Раздел:    " + tc.Section + "\n")
	sb.WriteString("Название:  " + tc.Name + "\n")
	if tc.PromptHint != "" {
		sb.WriteString("Контекст:  " + tc.PromptHint + "\n")
	}
	sb.WriteString("\nЗадача:\n" + tc.Task)
	return sb.String()
}

// buildSimplePrompt формирует промпт для одиночного и интерактивного режима.
// Контекст приложения уже встроен в системный промпт — здесь только задача.
func buildSimplePrompt(task string) string {
	return task
}

// buildReviewPrompt формирует запрос для второго прохода (ревью).
func buildReviewPrompt(originalPrompt, generatedCode string) string {
	return "=== ОРИГИНАЛЬНОЕ ЗАДАНИЕ ===\n" + originalPrompt +
		"\n\n=== СГЕНЕРИРОВАННЫЙ ТЕСТ ===\n" + generatedCode
}

// ─── Очистка ответа ──────────────────────────────────────────────────────────

// cleanCodeBlock убирает markdown-обёртку ```typescript / ```ts / ``` из ответа модели.
// Если бэктиков нет — возвращает строку как есть.
func cleanCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```typescript", "```ts", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			if idx := strings.LastIndex(s, "```"); idx != -1 {
				s = s[:idx]
			}
			return strings.TrimSpace(s)
		}
	}
	return s
}

// ─── Валидация ───────────────────────────────────────────────────────────────

// validateContent проверяет что ответ модели содержит корректный Playwright тест.
// Возвращает ошибку с перечислением всех найденных проблем.
func validateContent(content string) error {
	var issues []string

	if strings.TrimSpace(content) == "" {
		issues = append(issues, "пустой ответ")
	}
	if !strings.Contains(content, "test.describe") {
		issues = append(issues, "нет test.describe")
	}
	if !strings.Contains(content, "expect(") {
		issues = append(issues, "нет ни одного expect()")
	}
	if strings.Contains(content, "waitForTimeout") {
		issues = append(issues, "запрещённый waitForTimeout")
	}

	if len(issues) > 0 {
		return fmt.Errorf("валидация не пройдена: %s", strings.Join(issues, "; "))
	}
	return nil
}

// ─── Агентные промпты ─────────────────────────────────────────────────────────

// fixSystemPrompt — промпт для исправления упавшего теста.
const fixSystemPrompt = `Ты — senior QA Automation Engineer. Playwright тест упал с ошибкой.
Получишь: исходный код теста, вывод ошибки Playwright, и (если есть) список реальных
селекторов страницы из автосканирования.

ГЛАВНОЕ ПРАВИЛО: если есть раздел "ДОСТУПНЫЕ СЕЛЕКТОРЫ СТРАНИЦЫ" — используй ТОЛЬКО
селекторы из него. Не придумывай data-testid и aria-label которых там нет.

═══ ДИАГНОСТИКА ПО ТИПУ ОШИБКИ ═══

Timeout / Element not found:
  1. Посмотри раздел селекторов — найди правильный data-testid или aria-label для этого элемента
  2. Замени селектор: getByTestId('...') > getByRole('...', {name:'...'}) > getByLabel('...')
  3. Добавь явное ожидание перед действием:
     await page.waitForLoadState('networkidle');        // после навигации
     await element.waitFor({ state: 'visible' });       // перед кликом/fill
  4. Если элемент появляется после XHR — используй page.waitForResponse():
     await Promise.all([
       page.waitForResponse(resp => resp.url().includes('/api/...')),
       page.getByRole('button', { name: '...' }).click(),
     ]);

Порядок действий — всегда: navigate → wait → interact → expect:
  ❌  await el.fill(value);                           // нет ожидания
  ✅  await el.waitFor({ state: 'visible' });
      await el.fill(value);
      await expect(el).toHaveValue(value);

strict mode violation (найдено >1 элемента):
  Причина: один и тот же атрибут-селектор (name=..., type=submit, etc.) встречается в двух
  DOM-контейнерах (например, Google рендерит форму дважды — видимую и скрытую).
  Шаги исправления:
  1. Добавь .first() к локатору: page.locator('[name="btnI"]').first()
  2. Или уточни роль: getByRole('button', {name:'Submit', exact:true})
  3. Или добавь visible-фильтр: page.locator('[name="btnI"]:visible').first()

Expected visible, got hidden:
  → Проверь что page.goto() вызван с нужным URL
  → Добавь: await page.waitForLoadState('domcontentloaded')

Cannot read properties / locator is null:
  → Элемент не найден, нужен более точный селектор из раздела селекторов

Navigation / page closed:
  → Используй page.waitForURL() вместо устаревшего waitForNavigation:
     await Promise.all([
       page.waitForURL(url => !url.includes('old-domain.com')),
       triggerElement.click(),
     ]);

Element not visible после fill() / автокомплит скрывает кнопки:
  → После fill() сделай blur() чтобы закрыть выпадающий список:
     await input.fill(value);
     await input.blur();             // закрывает автокомплит
     await button.waitFor({ state: 'visible' });

waitForURL с RegExp и URL-спецсимволами:
  ? в URL (query string) — это regex-квантификатор, НЕЛЬЗЯ писать:
     ❌ page.waitForURL(new RegExp('/search?q=...'))   // ? делает h необязательным
  Вместо этого используй predicate-функцию или экранируй \?:
     ✅ page.waitForURL(url => url.includes('/search') && url.includes('q=...'))
     ✅ page.waitForURL(new RegExp('/search\\?q='))    // двойной \\ для escape в строке

═══ ЗАПРЕЩЕНО ═══
• waitForTimeout — всегда заменяй на waitFor / waitForResponse / waitForLoadState
• Придумывать селекторы которых нет в разделе "ДОСТУПНЫЕ СЕЛЕКТОРЫ СТРАНИЦЫ"
• Переписывать весь тест — исправляй только проблемное место
• new RegExp с URL-строкой без экранирования ? & + ( )

Ответ — только исправленный TypeScript код без пояснений и без markdown-блоков.`

// pomBatchSystemPrompt — промпт для post-batch POM рефакторинга.
const pomBatchSystemPrompt = `Ты — senior QA Automation Engineer.
Получишь несколько Playwright тестов из одного проекта.
Выдели общие паттерны и отрефактори их под Page Object Model.

ПРАВИЛА:
• Создай Page Object класс для каждой уникальной страницы
• Класс содержит только локаторы (readonly Locator) и методы действий
• Методы: async login(email, password), async addToCart(), etc.
• В spec-файлах используй Page Object — не обращайся к page напрямую
• Каждый файл начинается с маркера: --- FILE: путь/к/файлу.ts ---
• Структура: pages/НазваниеPage.ts и specs/название.spec.ts
• Нет expect() в Page Object — только в spec-файлах
• Нет waitForTimeout, console.log, any, TODO

ФОРМАТ ОТВЕТА — строго:
--- FILE: pages/LoginPage.ts ---
<код>
--- FILE: specs/login.spec.ts ---
<код>`

// buildFixPrompt формирует запрос для исправления упавшего теста.
// scanHint — контекст страницы (реальные селекторы + API) из фазы сканирования;
// если передан, добавляется отдельным разделом чтобы LLM не угадывал селекторы.
func buildFixPrompt(testCode, errorOutput, scanHint string) string {
	var sb strings.Builder
	sb.WriteString("=== КОД ТЕСТА ===\n")
	sb.WriteString(testCode)
	sb.WriteString("\n\n=== ОШИБКА PLAYWRIGHT ===\n")
	sb.WriteString(errorOutput)
	if scanHint != "" {
		sb.WriteString("\n\n=== ДОСТУПНЫЕ СЕЛЕКТОРЫ СТРАНИЦЫ ===\n")
		sb.WriteString(scanHint)
		sb.WriteString("\nИспользуй только эти селекторы при исправлении.\n")
	}
	return sb.String()
}

// buildPOMBatchPrompt формирует запрос для post-batch POM рефакторинга.
// files — map[имя файла]содержимое.
func buildPOMBatchPrompt(files map[string]string) string {
	var sb strings.Builder
	sb.WriteString("Отрефактори следующие тесты под Page Object Model.\n\n")
	for name, code := range files {
		sb.WriteString(fmt.Sprintf("--- FILE: %s ---\n%s\n\n", name, code))
	}
	return sb.String()
}
