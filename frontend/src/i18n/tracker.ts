export const trackerEn = {
  heading: "Yandex Tracker",
  intro:
    "Import a selection, then keep mapped milestones and dependencies synchronized. New issues require a confirmed import.",
  enabled: "Enable integration",
  org: "Organization ID",
  orgType: "Organization type",
  auth: "Authentication",
  secret:
    "The administrator supplies ROADMAP_TRACKER_TOKEN on the server. Credentials are never sent to this browser.",
  interval: "Polling interval (seconds)",
  selections: "Issue selections",
  selection: "Selection",
  queue: "Queue key",
  query: "Search query",
  project: "Roadmap project",
  choose: "Choose a project",
  remove: "Remove",
  addSelection: "Add selection",
  statuses: "Status mappings",
  statusHint:
    "Map successful completion and cancellation separately. A closed issue is not automatically completed. Enable one outgoing status per local status.",
  remoteStatus: "Tracker status key",
  localStatus: "Roadmap status",
  writeStatus: "Use for outgoing changes",
  addStatus: "Add status",
  mappings: "Field and dependency mappings",
  fieldHint:
    "Use field IDs available in your organization. Dates must be date-only fields. Person and version values use stable IDs. Multi-value versions cannot be represented by one milestone.",
  trackerField: "Tracker field ID",
  format: "Value format",
  allowWrite: "Allow outgoing changes",
  mapField: "Map field",
  localOnly: "Local only (not mapped)",
  readOnly: "Read from Tracker only",
  bidirectional: "Read and write",
  links: "Dependencies",
  link: "Link mapping",
  directionHint:
    "The configured direction means the issue being queried is the prerequisite. The inverse direction reverses the edge. Outgoing relationship is relative to that prerequisite. Verify your organization’s link types before enabling writes.",
  linkType: "Tracker link type ID",
  direction: "Prerequisite direction",
  relationship: "Roadmap relationship",
  requires: "Hard dependency",
  related: "Related",
  writeRelationship: "Outgoing Tracker relationship",
  addLink: "Add link mapping",
  save: "Save Tracker settings",
  test: "Test saved connection",
  preview: "Preview import",
  previewHint:
    "No issues are created or modified by preview. Confirming applies this snapshot to the local database.",
  projects: "Projects",
  milestones: "Milestones",
  skipped: "Skipped issues",
  warnings: "Warnings and links outside scope",
  confirm: "Confirm import",
  lastSuccess: "Last successful synchronization",
  running: "Synchronizing…",
  sync: "Synchronize",
  retry: "Reset failed operations for retry",
  state: "Synchronization",
  syncConflict: "Synchronization conflict",
  baseline: "Last synchronized value",
  keepLocal: "Keep Roadmap value",
  keepRemote: "Keep Tracker value",
  resolve: "Resolve selected values",
  unlink: "Unlink and exclude from future imports",
  mappingHint:
    "Unmapped fields remain local. Read-only mapped fields must be changed in Tracker or unlinked first.",
  destination: "Destination queue",
  publish: "Publish to Tracker",
  saved: "Tracker settings saved.",
  connected: "Connection succeeded.",
  scheduled: "Synchronization scheduled. Status refreshes automatically.",
  imported: "Import applied.",
  fields: {
    title: "Title",
    description: "Description",
    planned_date: "Planned date",
    completed_date: "Completion date",
    status: "Status",
    owner: "Responsible person",
    version: "Version",
  },
  states: {
    synchronized: "Synchronized",
    pending_upload: "Pending upload",
    error: "Error",
    conflict: "Conflict",
    unlinked: "Unlinked",
    lost_access: "Lost access",
    missing_or_inaccessible: "Missing or inaccessible",
    deleted: "Confirmed deleted",
    out_of_scope: "Outside selection",
    planned: "Planned",
    in_progress: "In progress",
    completed: "Completed",
    delayed: "Delayed",
    cancelled: "Cancelled",
  },
};
export const trackerRu: typeof trackerEn = {
  heading: "Яндекс Трекер",
  intro:
    "Импортируйте выборку и синхронизируйте настроенные поля и зависимости. Новые задачи требуют подтверждения импорта.",
  enabled: "Включить интеграцию",
  org: "ID организации",
  orgType: "Тип организации",
  auth: "Аутентификация",
  secret:
    "Администратор задаёт ROADMAP_TRACKER_TOKEN на сервере. Секрет не передаётся в браузер.",
  interval: "Интервал опроса (секунды)",
  selections: "Выборки задач",
  selection: "Выборка",
  queue: "Ключ очереди",
  query: "Поисковый запрос",
  project: "Проект дорожной карты",
  choose: "Выберите проект",
  remove: "Удалить",
  addSelection: "Добавить выборку",
  statuses: "Соответствия статусов",
  statusHint:
    "Настройте успешное завершение и отмену отдельно. Закрытая задача не считается завершённой автоматически. Для каждого локального статуса выберите один исходящий статус.",
  remoteStatus: "Ключ статуса Трекера",
  localStatus: "Статус дорожной карты",
  writeStatus: "Использовать для отправки",
  addStatus: "Добавить статус",
  mappings: "Соответствия полей и зависимостей",
  fieldHint:
    "Укажите ID полей вашей организации. Даты должны содержать только календарную дату. Ответственный и версия используют стабильные ID. Несколько версий нельзя представить одной вехой.",
  trackerField: "ID поля Трекера",
  format: "Формат значения",
  allowWrite: "Разрешить отправку изменений",
  mapField: "Настроить поле",
  localOnly: "Только локально (не настроено)",
  readOnly: "Только чтение из Трекера",
  bidirectional: "Чтение и запись",
  links: "Зависимости",
  link: "Соответствие связи",
  directionHint:
    "Настроенное направление означает, что запрашиваемая задача — предпосылка. Обратное направление разворачивает стрелку. Исходящая связь задаётся относительно предпосылки. Проверьте типы связей организации перед включением записи.",
  linkType: "ID типа связи Трекера",
  direction: "Направление предпосылки",
  relationship: "Связь дорожной карты",
  requires: "Обязательная зависимость",
  related: "Информационная связь",
  writeRelationship: "Исходящая связь Трекера",
  addLink: "Добавить тип связи",
  save: "Сохранить настройки Трекера",
  test: "Проверить сохранённое подключение",
  preview: "Предпросмотр импорта",
  previewHint:
    "Предпросмотр не создаёт и не изменяет задачи. Подтверждение применяет снимок к локальной базе.",
  projects: "Проекты",
  milestones: "Вехи",
  skipped: "Пропущенные задачи",
  warnings: "Предупреждения и связи вне выборки",
  confirm: "Подтвердить импорт",
  lastSuccess: "Последняя успешная синхронизация",
  running: "Синхронизация…",
  sync: "Синхронизировать",
  retry: "Сбросить ошибки для повтора",
  state: "Синхронизация",
  syncConflict: "Конфликт синхронизации",
  baseline: "Последнее синхронизированное значение",
  keepLocal: "Сохранить значение дорожной карты",
  keepRemote: "Сохранить значение Трекера",
  resolve: "Применить выбранные значения",
  unlink: "Отключить связь и исключить из импорта",
  mappingHint:
    "Ненастроенные поля остаются локальными. Поля только для чтения меняются в Трекере или после отключения связи.",
  destination: "Очередь назначения",
  publish: "Опубликовать в Трекере",
  saved: "Настройки Трекера сохранены.",
  connected: "Подключение успешно.",
  scheduled:
    "Синхронизация запланирована. Состояние обновляется автоматически.",
  imported: "Импорт применён.",
  fields: {
    title: "Название",
    description: "Описание",
    planned_date: "Плановая дата",
    completed_date: "Дата завершения",
    status: "Статус",
    owner: "Ответственный",
    version: "Версия",
  },
  states: {
    synchronized: "Синхронизировано",
    pending_upload: "Ожидает отправки",
    error: "Ошибка",
    conflict: "Конфликт",
    unlinked: "Не связано",
    lost_access: "Нет доступа",
    missing_or_inaccessible: "Не найдена или недоступна",
    deleted: "Удаление подтверждено",
    out_of_scope: "Вне выборки",
    planned: "Запланировано",
    in_progress: "В работе",
    completed: "Завершено",
    delayed: "Задержано",
    cancelled: "Отменено",
  },
};

// Backend diagnostics remain stable for API clients; translate them at the UI boundary.
const messagesRu: Record<string, string> = {
 "This database is already bound to a Tracker organization":"Эта база уже привязана к организации Трекера",
  "Use ISO YYYY-MM-DD calendar dates":
    "Укажите календарную дату в формате YYYY-MM-DD",
  "Title and project are required": "Нужны название и проект",
  "Completed milestone requires actual completion date":
    "Для завершённой вехи нужна фактическая дата завершения",
  "Invalid milestone status": "Недопустимый статус вехи",
  "Invalid milestone type": "Недопустимый тип вехи",
  "Field too long": "Превышена допустимая длина поля",
  "unmapped Tracker status": "Для статуса Трекера не задано соответствие",
  "ROADMAP_TRACKER_TOKEN is not configured":
    "На сервере не задан ROADMAP_TRACKER_TOKEN",
  "Tracker integration disabled": "Интеграция с Трекером отключена",
  "Tracker network failure; operation will be reconciled before retry":
    "Сбой сети Трекера; перед повтором состояние будет проверено",
  "Tracker response interrupted": "Ответ Трекера прерван",
  "invalid Tracker JSON": "Некорректный ответ JSON от Трекера",
  "Tracker requested delayed retry": "Трекер запросил отложенный повтор",
  "Tracker retry limit": "Достигнут предел повторов запросов к Трекеру",
  "unsafe pagination destination": "Недопустимый адрес следующей страницы",
  "unsafe API path": "Недопустимый путь API",
  "invalid API path": "Некорректный путь API",
  "invalid Tracker request": "Некорректный запрос к Трекеру",
  "pagination loop": "Зацикливание страниц",
  "pagination limit exceeded": "Достигнут предел числа страниц",
  "selection exceeds 10000 issues; narrow the query":
    "Выборка превышает 10 000 задач; сузьте запрос",
  "Preview expired or local data/configuration changed; preview again":
    "Предпросмотр устарел или локальные данные/настройки изменились; повторите предпросмотр",
  "Database error": "Ошибка базы данных",
  "Invalid preview": "Некорректный предпросмотр",
  "Synchronization already running": "Синхронизация уже выполняется",
  "synchronization already running": "Синхронизация уже выполняется",
  "Unlink imported milestones before changing organization or field/status/link mappings":
    "Отключите связи импортированных вех перед изменением организации или соответствий",
  "Pending publications must finish or be unlinked before changing mappings":
    "Дождитесь публикаций или отключите их перед изменением соответствий",
  "Select an enabled queue destination":
    "Выберите очередь назначения во включённой интеграции",
  "Milestone not found": "Веха не найдена",
  "Destination project differs from milestone project":
    "Проект назначения отличается от проекта вехи",
  "Milestone already linked or excluded; use a different milestone":
    "Веха уже связана или исключена; выберите другую",
  "Publication requires a write status mapping for this milestone":
    "Для публикации настройте исходящий статус этой вехи",
  "Cannot queue publication": "Не удалось поставить публикацию в очередь",
  "No synchronization conflict": "Нет конфликта синхронизации",
  "Choose a value for every disputed field":
    "Выберите значение для каждого спорного поля",
  "Local value changed; synchronize again":
    "Локальное значение изменилось; повторите синхронизацию",
  "invalid organization header or authentication":
    "Некорректный тип организации или аутентификации",
  "organization and polling interval (30–86400 seconds) required":
    "Нужны организация и интервал опроса от 30 до 86400 секунд",
  "configure 1–50 selections": "Настройте от 1 до 50 выборок",
  "summary title and planned date mappings required":
    "Настройте название через summary и поле плановой даты",
  "status requires status codec": "Для статуса нужен формат status",
  "dates require date codec": "Для дат нужен формат date",
  "reserved Tracker field": "Зарезервированное поле Трекера",
  "invalid local status": "Некорректный локальный статус",
  "write status must map back to local status":
    "Исходящий статус должен иметь обратное соответствие локальному",
  "choose queue or query for each selection":
    "Укажите очередь или запрос в каждой выборке",
  "selection requires a project": "Для выборки нужен проект",
  "choose existing or new project": "Выберите существующий или новый проект",
  "invalid or duplicate link mapping":
    "Некорректное или неоднозначное соответствие связей",
  "invalid version field": "Некорректное поле версии",
  "issue identity/version missing":
    "В ответе нет идентификатора или версии задачи",
  "selection project not found": "Проект выборки не найден",
  "stable issue ID changed": "Стабильный ID задачи изменился",
  "issue no longer matches selections; uploads paused":
    "Задача больше не входит в выборки; отправка приостановлена",
  "outgoing operation waiting for retry": "Исходящая операция ожидает повтора",
  "outgoing retry limit reached; reset failed operations":
    "Достигнут предел повторов; сбросьте ошибки операций",
  "status changed before transition; reconcile again":
    "Статус изменился перед переходом; повторите синхронизацию",
  "ambiguous status transition": "Неоднозначный переход статуса",
  "status transition unavailable or requires workflow configuration":
    "Переход статуса недоступен или требует настройки процесса",
  "some issues need attention; inspect synchronization records":
    "Некоторые задачи требуют внимания; проверьте записи синхронизации",
  "duplicate publication identity":
    "Обнаружен повтор идентификатора публикации",
  "outgoing link operation waiting for retry":
    "Исходящая операция связи ожидает повтора",
  "unknown link direction": "Неизвестное направление связи",
  "link pagination limit": "Достигнут предел числа страниц связей",
};
export function trackerMessage(message: string, lang: string): string {
  if (lang !== "ru") return message;
  const raw = message.replace(/^Error: /, "");
  if (messagesRu[raw]) return messagesRu[raw];
  const prefixes: Record<string, string> = {
    "Tracker HTTP ": "Трекер HTTP ",
    "hard dependency cycle: ": "Цикл обязательных зависимостей: ",
    "self dependency: ": "Зависимость от самой себя: ",
    "linked issue outside selection: ": "Связанная задача вне выборки: ",
    "invalid or duplicate field mapping: ":
      "Некорректное или повторное соответствие поля: ",
    "local change to read-only field: ":
      "Локальное изменение поля только для чтения: ",
    "no write status mapping: ": "Нет исходящего соответствия статуса: ",
    "Choose local or remote for ":
      "Выберите локальное значение или значение Трекера для ",
    "Import rejected: ": "Импорт отклонён: ",
  };
  for (const [prefix, translation] of Object.entries(prefixes)) {
    if (raw.startsWith(prefix)) return translation + raw.slice(prefix.length);
  }
  let m = raw.match(/^field (.+) requires ISO date-only data$/);
  if (m) return `Поле ${m[1]} должно содержать дату YYYY-MM-DD`;
  m = raw.match(/^field (.+) requires zero or one version$/);
  if (m) return `В поле ${m[1]} допускается не более одной версии`;
  m = raw.match(/^field (.+) is not a scalar string$/);
  if (m) return `Поле ${m[1]} должно содержать строку`;
  m = raw.match(/^issue (.+) matches selections with different projects$/);
  if (m) return `Задача ${m[1]} попала в выборки разных проектов`;
  return `Ошибка синхронизации. Технические сведения: ${raw}`;
}
