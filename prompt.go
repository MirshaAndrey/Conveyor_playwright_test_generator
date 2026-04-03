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
