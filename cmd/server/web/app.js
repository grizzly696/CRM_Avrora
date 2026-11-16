const state = {
  contacts: [],
  files: [],
  messages: [],
  admin: { users: [], roles: [], audit: [], overview: {} },
  view: 'overview',
  role: 'manager',
  selectedFile: null,
  stats: {},
  user: null,
  vulnerabilities: [],
  tasks: { items: [], users: [], groups: [] },
  departmentItems: [],
  employees: [],
  drive: { folderId: 0, folders: [], files: [] },
  chats: { items: [], users: [] },
  activeChat: null,
};

const $ = (selector) => document.querySelector(selector);

const money = new Intl.NumberFormat('ru-RU', {
  style: 'currency',
  currency: 'RUB',
  maximumFractionDigits: 0,
});

async function request(url, options) {
  const response = await fetch(url, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || 'Не удалось выполнить запрос');
  }
  return response.json();
}

function showToast(message, isError = false) {
  const toast = $('#toast');
  toast.textContent = message;
  toast.className = `toast visible${isError ? ' error' : ''}`;
  window.setTimeout(() => { toast.className = 'toast'; }, 2800);
}

function statusLabel(status) {
  return status === 'active' ? 'Активный' : 'Лид';
}

const roles = {
  manager: {
    label: 'Менеджмент', title: 'Центр управления командой', description: 'Задачи отделов, клиенты и ключевые показатели команды.',
    items: [['Проверить работу отделов', 'Сводка процессов и статусов', 'department'], ['Назначить задачу команде', 'Поручения по ролям и сотрудникам', 'tasks'], ['Посмотреть клиентскую воронку', 'Контакты и коммерческие возможности', 'pipeline']],
  },
  hr: {
    label: 'Кадры · HR', title: 'Рабочее место HR', description: 'Подбор, адаптация, отпуска и кадровые документы.',
    items: [['Вести вакансии и кандидатов', 'Этапы подбора и обратная связь', 'department'], ['Организовать адаптацию', 'План выхода и контрольные точки', 'department'], ['Обрабатывать кадровые запросы', 'Отпуска и документы сотрудников', 'department']],
  },
  developer: {
    label: 'Разработка', title: 'Рабочее место разработчика', description: 'Бэклог, дефекты, релизы и технический долг.',
    items: [['Разбирать задачи и дефекты', 'Бэклог и приоритеты команды', 'tasks'], ['Готовить релизы', 'Контроль изменений и готовности', 'tasks'], ['Документировать технические решения', 'Материалы команды на общем диске', 'drive']],
  },
  security: {
    label: 'Информационная безопасность', title: 'Рабочее место специалиста ИБ', description: 'Уязвимости, инциденты, доступы и проверки безопасности.',
    items: [['Проверять уязвимости', 'Критичность, статус и владелец устранения', 'vulnerabilities'], ['Фиксировать инциденты и риски', 'Единый журнал рабочих проверок', 'department'], ['Контролировать доступы', 'Проверки прав на файлы и чаты', 'drive']],
  },
  accountant: {
    label: 'Бухгалтерия', title: 'Рабочее место бухгалтера', description: 'Счета, платежи, сверки и финансовая отчётность.',
    items: [['Вести счета и платежи', 'Контрагент, сумма и срок оплаты', 'department'], ['Проводить сверки', 'Статусы операций и расхождения', 'department'], ['Готовить отчёты', 'Рабочие документы и контроль сроков', 'drive']],
  },
  marketing: {
    label: 'Маркетинг', title: 'Рабочее место маркетолога', description: 'Кампании, контент-план, аналитика и маркетинговые лиды.',
    items: [['Планировать кампании', 'Аудитория, каналы, бюджет и KPI', 'department'], ['Вести контент-план', 'Материалы и календарь публикаций', 'department'], ['Оценивать результаты', 'Метрики и качество лидов', 'department']],
  },
  sales: {
    label: 'Продажи', title: 'Рабочее место специалиста по продажам', description: 'Клиенты, лиды, коммерческие предложения и воронка сделок.',
    items: [['Обрабатывать контакты и лиды', 'История и данные компаний в CRM', 'contacts'], ['Вести сделки по этапам', 'Потенциал и состояние воронки', 'pipeline'], ['Планировать следующие шаги', 'Звонки, предложения и задачи', 'tasks']],
  },
};

function renderRole() {
  const role = roles[state.role];
  $('#role-title').textContent = role.title;
  $('#role-description').textContent = role.description;
  $('#role-badge').textContent = role.label;
  $('#user-name').textContent = state.user.name;
  $('#user-role').textContent = role.label;
  document.querySelectorAll('.avatar-small').forEach((avatar) => { avatar.textContent = state.user.name.split(/\s+/).slice(0, 2).map((part) => part[0]).join('').toUpperCase(); });
  $('#role-content').innerHTML = role.items.map(([title, detail, action]) => `
    <div class="role-task"><span class="task-check" aria-hidden="true">○</span><div><strong>${title}</strong><small>${detail}</small></div><button class="role-action" data-role-action="${action}">Открыть</button></div>`).join('');
  $('#vulnerability-workspace').hidden = state.role !== 'security';
  if (state.role === 'security') loadVulnerabilities();
}

const roleKeys = { 'Менеджмент': 'manager', 'Кадры': 'hr', 'Разработка': 'developer', 'Информационная безопасность': 'security', 'Бухгалтерия': 'accountant', 'Маркетинг': 'marketing', 'Продажи': 'sales' };

function showAuthenticated(user) {
  state.user = user;
  state.role = roleKeys[user.role] || 'manager';
  $('#auth-screen').hidden = true;
  $('#app-shell').hidden = false;
  const adminNav = document.querySelector('[data-view="admin"]');
  if (adminNav) adminNav.hidden = !user.isAdmin;
  renderRole();
  setView('overview');
  loadData();
}

function showLogin(message = '') {
  $('#app-shell').hidden = true;
  $('#auth-screen').hidden = false;
  $('#login-error').textContent = message;
}

async function startAuth() {
  try {
    const user = await request('/api/auth/me');
    showAuthenticated(user);
  } catch (error) {
    showLogin();
  }
}

function renderContacts() {
  const query = $('#search').value.trim().toLowerCase();
  const status = $('#status').value;
  const visible = state.contacts.filter((contact) => {
    const haystack = `${contact.name} ${contact.company} ${contact.email}`.toLowerCase();
    return (!query || haystack.includes(query)) && (!status || contact.status === status);
  });
  const list = $('#contacts-list');
  list.innerHTML = visible.length ? visible.map((contact) => `
    <tr>
      <td><div class="contact-cell"><span class="contact-avatar">${contact.name.slice(0, 1)}</span><div><strong>${escapeHTML(contact.name)}</strong><small>${escapeHTML(contact.email || 'Без email')}</small></div></div></td>
      <td>${escapeHTML(contact.company || '—')}</td>
      <td><span class="status ${contact.status}">${statusLabel(contact.status)}</span></td>
      <td class="value">${money.format(contact.value)}</td>
      <td><button class="row-action" data-contact-open="${contact.id}" aria-label="Открыть контакт">→</button></td>
    </tr>`).join('') : '<tr><td colspan="5" class="empty">Контакты не найдены</td></tr>';
  $('#nav-count').textContent = state.contacts.length;
  renderFullContacts();
}

function renderFullContacts() {
  if (!$('#contacts-full-list')) return;
  const query = $('#full-search').value.trim().toLowerCase();
  const status = $('#full-status').value;
  const visible = state.contacts.filter((contact) => {
    const haystack = `${contact.name} ${contact.company} ${contact.email}`.toLowerCase();
    return (!query || haystack.includes(query)) && (!status || contact.status === status);
  });
  $('#contacts-full-list').innerHTML = visible.length ? visible.map((contact) => `
    <tr><td><div class="contact-cell"><span class="contact-avatar">${escapeHTML(contact.name.slice(0, 1))}</span><div><strong>${escapeHTML(contact.name)}</strong><small>${escapeHTML(contact.email || 'Без email')}</small></div></div></td><td>${escapeHTML(contact.company || '—')}</td><td>${escapeHTML(contact.email || '—')}</td><td><span class="status ${contact.status}">${statusLabel(contact.status)}</span></td><td class="value">${money.format(contact.value)}</td><td class="last-seen">${formatDate(contact.createdAt)}</td></tr>`).join('') : '<tr><td colspan="6" class="empty">Контакты не найдены</td></tr>';
  $('#contacts-insight-count').textContent = state.stats.contacts || state.contacts.length;
  $('#contacts-insight-active').textContent = state.stats.active || 0;
  $('#contacts-insight-leads').textContent = state.stats.leads || 0;
  $('#contacts-insight-value').textContent = money.format(state.stats.pipeline || 0);
}

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (character) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#039;',
  }[character]));
}

function renderStats(stats) {
  $('#stat-contacts').textContent = stats.contacts;
  $('#stat-active').textContent = stats.active;
  $('#stat-leads').textContent = stats.leads;
  $('#stat-pipeline').textContent = money.format(stats.pipeline);
  renderPipeline();
}

function renderPipeline() {
  if (!$('#pipeline-board')) return;
  const stages = [
    { key: 'lead', title: 'Новые лиды', note: 'Первичный контакт', tone: 'blue' },
    { key: 'active', title: 'В работе', note: 'Активные сделки', tone: 'green' },
  ];
  $('#pipeline-total').textContent = money.format(state.stats.pipeline || 0);
  $('#pipeline-board').innerHTML = stages.map((stage) => {
    const contacts = state.contacts.filter((contact) => contact.status === stage.key);
    return `<section class="pipeline-column"><div class="pipeline-column-head"><div><span class="pipeline-dot ${stage.tone}"></span><strong>${stage.title}</strong><small>${stage.note}</small></div><b>${contacts.length}</b></div><div class="pipeline-cards">${contacts.length ? contacts.map((contact) => `<article class="deal-card"><div class="deal-top"><span class="contact-avatar">${escapeHTML(contact.name.slice(0, 1))}</span><span class="status ${contact.status}">${statusLabel(contact.status)}</span></div><strong>${escapeHTML(contact.name)}</strong><small>${escapeHTML(contact.company || 'Без компании')}</small><div class="deal-bottom"><b>${money.format(contact.value)}</b><button class="row-action" data-contact-open="${contact.id}" aria-label="Открыть контакт">→</button></div></article>`).join('') : '<div class="pipeline-empty">Нет сделок на этой стадии</div>'}</div></section>`;
  }).join('');
}

function renderFiles() {
  renderFileCollection('#files-list', '#file-search', '#file-kind');
  renderFileCollection('#files-full-list', '#full-file-search', '#full-file-kind');
}

function renderDrive() {
  state.files = state.drive.files;
  renderFiles();
  $('#manage-folder-access').hidden = !state.drive.canManageAccess;
  $('#drive-folders').innerHTML = state.drive.folders.map((folder) => `<button class="drive-folder" data-folder-id="${folder.id}"><span>▣</span><strong>${escapeHTML(folder.name)}</strong><small>Открыть папку →</small></button>`).join('') || '<div class="empty">В этой папке нет вложенных папок</div>';
  $('#drive-breadcrumbs').textContent = state.drive.folderId ? `Общий диск / Папка ${state.drive.folderId}` : 'Общий диск';
}

async function loadDrive(folderId = 0) {
  try { state.drive = await request(`/api/drive?folderId=${folderId}`); renderDrive(); } catch (error) { showToast(error.message, true); }
}

function renderChats() {
  $('#chats-list').innerHTML = state.chats.items.length ? state.chats.items.map((chat) => `<button class="chat-item ${state.activeChat?.id === chat.id ? 'active' : ''}" data-chat-id="${chat.id}"><strong>${escapeHTML(chat.name)}</strong><small>${escapeHTML(chat.lastMessage || 'Нет сообщений')}</small></button>`).join('') : '<div class="empty">Чатов пока нет</div>';
  $('#chat-members').innerHTML = state.chats.users.filter((user) => user.id !== state.user?.id).map((user) => `<option value="${user.id}">${escapeHTML(user.name)} · ${escapeHTML(user.role)}</option>`).join('');
  $('#chat-member-select').innerHTML = state.chats.users.filter((user) => user.id !== state.user?.id).map((user) => `<option value="${user.id}">${escapeHTML(user.name)} · ${escapeHTML(user.role)}</option>`).join('');
}

async function loadChats() {
  try { const data = await request('/api/chats'); state.chats = { items: data.chats, users: data.users }; renderChats(); if (state.activeChat && state.chats.items.some((chat) => chat.id === state.activeChat.id)) { await selectChat(state.activeChat.id); } else if (state.chats.items[0]) { await selectChat(state.chats.items[0].id); } } catch (error) { showToast(error.message, true); }
}

async function selectChat(chatId) {
  const chat = state.chats.items.find((item) => item.id === Number(chatId));
  if (!chat) return;
  try { const messages = await request(`/api/chats/${chat.id}/messages`); state.activeChat = chat; renderChats(); $('#chat-title').textContent = chat.name; $('#chat-meta').textContent = `${messages.length} сообщений`; $('#messages-full-list').innerHTML = messages.map((message) => `<div class="full-message"><span class="contact-avatar">${escapeHTML(message.author.slice(0, 1))}</span><div><strong>${escapeHTML(message.author)} <small>${escapeHTML(message.role)}</small></strong><p>${escapeHTML(message.text)}</p><time>${formatDate(message.createdAt)}</time></div></div>`).join(''); $('#message-full-form input').disabled = false; $('#message-full-form button').disabled = false; } catch (error) { showToast(error.message, true); }
}

function renderFileCollection(listSelector, searchSelector, kindSelector) {
  if (!$(listSelector)) return;
  const query = $(searchSelector).value.trim().toLowerCase();
  const kind = $(kindSelector).value;
  const office = ['DOC', 'DOCX', 'PDF', 'XLS', 'XLSX', 'PPT', 'PPTX', 'ODT', 'ODS'];
  const visible = state.files.filter((file) => {
    const matchesText = !query || `${file.name} ${file.kind} ${file.owner}`.toLowerCase().includes(query);
    const isCode = ['GO', 'JS', 'TS', 'PY', 'JAVA', 'HTML', 'CSS', 'SQL', 'JSON', 'MD'].includes(file.kind);
    const matchesKind = !kind || (kind === 'code' && isCode) || (kind === 'office' && office.includes(file.kind));
    return matchesText && matchesKind;
  });
  $(listSelector).innerHTML = visible.length ? visible.map((file) => `
    <div class="file-row"><span class="file-icon">${escapeHTML(file.kind || 'FILE')}</span><div><strong>${escapeHTML(file.name)}</strong><small>${escapeHTML(file.owner || 'Команда')} · ${escapeHTML(file.size || 'размер не указан')}</small></div><button class="row-action" data-file-open="${file.id}" aria-label="Открыть файл">→</button></div>`).join('') : '<div class="empty file-empty">Файлы не найдены</div>';
}

function renderMessages() {
  $('#messages-list').innerHTML = state.messages.slice(-3).map((message) => `
    <div class="message-row"><span class="contact-avatar">${escapeHTML(message.author.slice(0, 1))}</span><div><strong>${escapeHTML(message.author)} <small>${escapeHTML(message.role)}</small></strong><p>${escapeHTML(message.text)}</p></div></div>`).join('');
  if ($('#messages-full-list')) {
    $('#messages-full-list').innerHTML = state.messages.map((message) => `
      <div class="full-message"><span class="contact-avatar">${escapeHTML(message.author.slice(0, 1))}</span><div><strong>${escapeHTML(message.author)} <small>${escapeHTML(message.role)}</small></strong><p>${escapeHTML(message.text)}</p><time>${formatDate(message.createdAt)}</time></div></div>`).join('');
    if ($('#full-message-count')) $('#full-message-count').textContent = state.messages.length;
  }
}

function renderTasks() {
  const labels = { critical: 'Критический', high: 'Высокий', medium: 'Средний', low: 'Низкий' };
  const statuses = { todo: 'К выполнению', in_progress: 'В работе', done: 'Завершено' };
  const query = $('#task-search').value.trim().toLowerCase();
  const status = $('#task-status-filter').value;
  const priority = $('#task-priority-filter').value;
  const items = state.tasks.items.filter((task) => (!query || `${task.title} ${task.description} ${task.assignee} ${task.groupRole}`.toLowerCase().includes(query)) && (!status || task.status === status) && (!priority || task.priority === priority));
  $('#task-count').textContent = state.tasks.items.filter((task) => task.status !== 'done').length;
  $('#tasks-total').textContent = state.tasks.items.length;
  $('#tasks-progress').textContent = state.tasks.items.filter((task) => task.status === 'in_progress').length;
  $('#tasks-done').textContent = state.tasks.items.filter((task) => task.status === 'done').length;
  $('#tasks-list').innerHTML = items.length ? items.map((task) => {
    const target = task.assignee ? task.assignee : task.groupRole ? `Группа: ${task.groupRole}` : 'Не назначена';
    const canClaim = task.status === 'todo' && (!task.assigneeId || task.assigneeId === state.user?.id) && (!task.groupRole || task.groupRole === state.user?.role);
    const canFinish = task.status === 'in_progress' && task.assigneeId === state.user?.id;
    return `<article class="task-row"><div class="task-priority ${task.priority}"></div><div class="task-main"><div class="task-title-line"><strong>${escapeHTML(task.title)}</strong><span class="task-status ${task.status}">${statuses[task.status]}</span></div><p>${escapeHTML(task.description || 'Описание не добавлено')}</p><small>${escapeHTML(target)} · ${labels[task.priority]}</small></div><div class="task-actions">${canClaim ? `<button class="primary compact" data-task-claim="${task.id}">Взять в работу</button>` : ''}${canFinish ? `<button class="more" data-task-status="${task.id}" data-status-value="done">Завершить</button>` : ''}</div></article>`;
  }).join('') : '<div class="empty task-empty">Задачи не найдены</div>';
  const assignment = $('#task-assignment');
  if (assignment) assignment.innerHTML = '<option value="">Не назначать</option><optgroup label="Сотрудники">' + state.tasks.users.map((user) => `<option value="user:${user.id}">${escapeHTML(user.name)} · ${escapeHTML(user.role)}</option>`).join('') + '</optgroup><optgroup label="Группы">' + state.tasks.groups.map((group) => `<option value="group:${escapeHTML(group)}">Группа: ${escapeHTML(group)}</option>`).join('') + '</optgroup>';
}

function renderVulnerabilities() {
  const labels = { critical: 'Критическая', high: 'Высокая', medium: 'Средняя', low: 'Низкая' };
  const statuses = { open: 'Открыта', in_progress: 'В работе', resolved: 'Устранена' };
  $('#vulnerability-list').innerHTML = state.vulnerabilities.length ? state.vulnerabilities.map((item) => `
    <article class="vulnerability-row"><span class="severity ${item.severity}">${labels[item.severity]}</span><div class="vulnerability-main"><strong>${escapeHTML(item.title)}</strong><p>${escapeHTML(item.description)}</p><small>${escapeHTML(item.owner)} · ${formatDate(item.createdAt)}</small></div><select class="vulnerability-status ${item.status}" data-vulnerability-status="${item.id}"><option value="open" ${item.status === 'open' ? 'selected' : ''}>${statuses.open}</option><option value="in_progress" ${item.status === 'in_progress' ? 'selected' : ''}>${statuses.in_progress}</option><option value="resolved" ${item.status === 'resolved' ? 'selected' : ''}>${statuses.resolved}</option></select></article>`).join('') : '<div class="empty">Уязвимости не зарегистрированы</div>';
}

async function loadVulnerabilities() {
  try { state.vulnerabilities = await request('/api/security/vulnerabilities'); renderVulnerabilities(); } catch (error) { $('#vulnerability-list').innerHTML = '<div class="empty">Реестр доступен только специалистам ИБ.</div>'; }
}

function formatDate(value) {
  return new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }).format(new Date(value));
}

function renderAdmin() {
  const { overview, users, roles: availableRoles, audit } = state.admin;
  $('#admin-users').textContent = overview.users || 0;
  $('#admin-active').textContent = overview.active || 0;
  $('#admin-inactive').textContent = overview.inactive || 0;
  $('#admin-roles').textContent = overview.roles || availableRoles.length;
  $('#admin-users-list').innerHTML = users.map((user) => `
    <tr><td><div class="contact-cell"><span class="contact-avatar">${escapeHTML(user.name.slice(0, 1))}</span><div><strong>${escapeHTML(user.name)}</strong><small>${escapeHTML(user.email)}</small></div></div></td><td><select class="admin-select" data-user-role="${user.id}">${availableRoles.map((role) => `<option ${role === user.role ? 'selected' : ''}>${escapeHTML(role)}</option>`).join('')}</select></td><td><select class="admin-select status-select ${user.status}" data-user-status="${user.id}"><option value="active" ${user.status === 'active' ? 'selected' : ''}>Активен</option><option value="inactive" ${user.status === 'inactive' ? 'selected' : ''}>Неактивен</option></select></td><td class="last-seen">${formatDate(user.lastSeen)}</td><td><button class="row-action" data-user-save="${user.id}" aria-label="Сохранить изменения">✓</button></td></tr>`).join('');
  $('#audit-list').innerHTML = audit.slice(0, 6).map((event) => `<div class="audit-row"><span class="audit-dot"></span><div><strong>${escapeHTML(event.action)}</strong><p>${escapeHTML(event.actor)} · ${escapeHTML(event.target)}</p><small>${formatDate(event.createdAt)}</small></div></div>`).join('');
}

const departmentRoles = ['Кадры', 'Разработка', 'Информационная безопасность', 'Бухгалтерия', 'Маркетинг', 'Продажи'];
const departmentKinds = {
  'Кадры': ['Вакансия', 'Кандидат', 'Адаптация', 'Отпуск', 'Документ'],
  'Разработка': ['Задача', 'Дефект', 'Релиз', 'Технический долг'],
  'Информационная безопасность': ['Проверка', 'Инцидент', 'Доступ', 'Риск'],
  'Бухгалтерия': ['Платёж', 'Счёт', 'Сверка', 'Отчёт'],
  'Маркетинг': ['Кампания', 'Контент', 'Аналитика', 'Лид'],
  'Продажи': ['Сделка', 'Звонок', 'Предложение', 'Задача'],
};

function renderDepartmentItems() {
  const isManager = state.user?.isAdmin || state.user?.role === 'Менеджмент';
  const role = state.user?.role;
  const roleField = $('#department-role-field');
  if (roleField) {
    roleField.hidden = !isManager;
    $('#department-role').innerHTML = departmentRoles.map((item) => `<option value="${escapeHTML(item)}">${escapeHTML(item)}</option>`).join('');
  }
  const title = role === 'Менеджмент' ? 'Работа всех отделов' : `Работа отдела · ${roles[state.role].label}`;
  $('#department-title').textContent = title;
  $('#department-eyebrow').textContent = isManager ? 'Управление · Отделы' : roles[state.role].label;
  $('#department-description').textContent = isManager
    ? 'Общие процессы по кадрам, разработке, безопасности, финансам, маркетингу и продажам.'
    : roles[state.role].description;
  const query = $('#department-search').value.trim().toLocaleLowerCase('ru');
  const isHR = role === 'Кадры';
  $('#employee-directory').hidden = !isHR && !isManager;
  if (isHR || isManager) renderEmployeeDirectory();
  const statusFilter = $('#department-status-filter').value;
  const items = state.departmentItems.filter((item) => {
    const text = `${item.title} ${item.description} ${item.kind} ${item.role}`.toLocaleLowerCase('ru');
    return (!query || text.includes(query)) && (!statusFilter || item.status === statusFilter);
  });
  $('#department-open').textContent = state.departmentItems.filter((item) => item.status === 'open').length;
  $('#department-progress').textContent = state.departmentItems.filter((item) => item.status === 'in_progress').length;
  $('#department-done').textContent = state.departmentItems.filter((item) => item.status === 'done').length;
  const statuses = { open: 'Открыто', in_progress: 'В работе', done: 'Завершено' };
  $('#department-items').innerHTML = items.length ? items.map((item) => `
    <article class="task-row"><div class="task-priority ${item.status === 'done' ? 'low' : item.status === 'in_progress' ? 'medium' : 'high'}"></div><div class="task-main"><div class="task-title-line"><strong>${escapeHTML(item.title)}</strong><span class="task-status ${item.status === 'open' ? 'todo' : item.status}">${statuses[item.status]}</span></div><p>${escapeHTML(item.description || 'Описание не добавлено')}</p><small>${escapeHTML(item.role)} · ${escapeHTML(item.kind)}${item.dueAt ? ` · Срок: ${escapeHTML(item.dueAt)}` : ''}${item.amount ? ` · ${money.format(item.amount)}` : ''}</small></div><div class="task-actions">${item.status !== 'done' ? `<button class="more" data-department-status="${item.id}" data-status-value="${item.status === 'open' ? 'in_progress' : 'done'}">${item.status === 'open' ? 'Взять в работу' : 'Завершить'}</button>` : ''}</div></article>`).join('') : '<div class="empty task-empty">Записей пока нет. Добавьте первую рабочую запись.</div>';
  const roleSelect = $('#department-role');
  if (roleSelect) renderDepartmentKinds(roleSelect.value || (isManager ? departmentRoles[0] : role));
}

function renderDepartmentKinds(role) {
  const kindSelect = $('#department-kind');
  if (!kindSelect) return;
  kindSelect.innerHTML = (departmentKinds[role] || []).map((kind) => `<option value="${escapeHTML(kind)}">${escapeHTML(kind)}</option>`).join('');
}

function renderEmployeeDirectory() {
  $('#employee-directory-list').innerHTML = state.employees.length ? state.employees.map((employee) => `
    <tr><td><strong>${escapeHTML(employee.name)}</strong></td><td>${escapeHTML(employee.email)}</td><td>${escapeHTML(employee.role)}</td><td><span class="status ${employee.status === 'active' ? 'active' : 'lead'}">${employee.status === 'active' ? 'Активен' : 'Неактивен'}</span></td></tr>`).join('') : '<tr><td colspan="4" class="empty">Сотрудников пока нет</td></tr>';
}

async function loadDepartmentItems() {
  try {
    state.departmentItems = await request('/api/department/items');
    renderDepartmentItems();
  } catch (error) {
    console.error(error);
    showToast(error.message, true);
  }
}

async function loadData() {
  try {
    const [contacts, stats, files, messages, tasks, drive, chats, departmentItems] = await Promise.all([
      request('/api/contacts'),
      request('/api/stats'),
      request('/api/workspace/files'),
      request('/api/workspace/messages'),
      request('/api/tasks'),
      request('/api/drive?folderId=0'),
      request('/api/chats'),
      request('/api/department/items'),
    ]);
    state.contacts = contacts;
    state.files = files;
    state.messages = messages;
    state.stats = stats;
    state.tasks = { items: tasks.tasks, users: tasks.users, groups: tasks.groups };
    state.departmentItems = departmentItems;
    if (state.role === 'hr' || state.role === 'manager' || state.user?.isAdmin) {
      state.employees = await request('/api/department/employees');
    }
    state.drive = drive;
    state.chats = { items: chats.chats, users: chats.users };
    renderContacts();
    renderStats(stats);
    renderFiles();
    renderMessages();
    renderTasks();
    renderDepartmentItems();
    renderDrive();
    renderChats();
    if (state.chats.items[0]) await selectChat(state.chats.items[0].id);
    if (state.user?.isAdmin) {
      const [adminOverview, adminUsers, audit] = await Promise.all([
        request('/api/admin/overview'),
        request('/api/admin/users'),
        request('/api/admin/audit'),
      ]);
      state.admin = { overview: adminOverview, users: adminUsers.users, roles: adminUsers.roles, audit };
      renderAdmin();
    }
  } catch (error) {
    console.error(error);
    showToast(error.message, true);
  }
}

function setView(view) {
  if (view === 'admin' && !state.user?.isAdmin) {
    showToast('Админ-панель доступна только администраторам', true);
    return;
  }
  state.view = view;
  const labels = { overview: 'Обзор', contacts: 'Контакты', pipeline: 'Воронка', drive: 'Общий диск', messenger: 'Мессенджер', tasks: 'Задачи', department: 'Мой отдел', admin: 'Админ-панель' };
  const titles = {
    overview: ['Добрый день, Алексей.', 'Центр управления командой и клиентами.'],
    contacts: ['Контакты команды.', 'Полная база отношений и коммерческого потенциала.'],
    pipeline: ['Воронка продаж.', 'Сделки и следующие шаги по клиентам.'],
    drive: ['Общий диск команды.', 'Документы, исходный код и рабочие материалы.'],
    messenger: ['Командный мессенджер.', 'Быстрые решения и рабочие обновления.'],
    tasks: ['Задачи команды.', 'Назначение, выполнение и контроль рабочего результата.'],
    department: ['Работа отдела.', 'Специализированные процессы, сроки и результаты команды.'],
    admin: ['Центр управления системой.', 'Настройка сотрудников, ролей и контроля доступа.'],
  };
  $('#page-label').textContent = labels[view];
  if (view === 'overview' && state.user) {
    titles.overview[0] = `Добрый день, ${state.user.name.split(/\s+/)[0]}.`;
    titles.overview[1] = roles[state.role].description;
  }
  const isAdmin = view === 'admin';
  const isOverview = view === 'overview';
  $('#admin-panel').hidden = !isAdmin;
  document.querySelector('.page-head').hidden = !isOverview;
  document.querySelectorAll('.role-panel, .stats-grid, .workspace-grid, .shared-grid').forEach((section) => { section.hidden = !isOverview; });
  document.querySelectorAll('.screen-view').forEach((section) => { section.hidden = true; });
  const screen = document.querySelector(`#${view}-screen`);
  if (screen) screen.hidden = false;
  $('#add-contact').hidden = !isOverview;
  $('#page-title').textContent = titles[view][0];
  $('#page-subtitle').textContent = titles[view][1];
  document.querySelectorAll('[data-view]').forEach((item) => {
    item.classList.toggle('active', item.dataset.view === view);
  });
  if (view === 'drive') {
    $('#full-file-search').focus();
    loadDrive(state.drive.folderId || 0);
  }
  if (view === 'messenger') {
    $('#message-full-form input').focus();
    if (!state.activeChat && state.chats.items[0]) selectChat(state.chats.items[0].id);
  }
  if (view === 'contacts') {
    $('#full-search').focus();
  }
    if (view === 'tasks') {
      $('#task-search').focus();
    }
}

$('#add-contact').addEventListener('click', () => $('#contact-dialog').showModal());
$('#add-file').addEventListener('click', () => $('#file-dialog').showModal());
$('#add-user').addEventListener('click', () => $('#user-dialog').showModal());
$('#search').addEventListener('input', renderContacts);
$('#status').addEventListener('change', renderContacts);
$('#file-search').addEventListener('input', renderFiles);
$('#file-kind').addEventListener('change', renderFiles);
$('#full-search').addEventListener('input', renderFullContacts);
$('#full-status').addEventListener('change', renderFullContacts);
$('#full-file-search').addEventListener('input', renderFiles);
$('#full-file-kind').addEventListener('change', renderFiles);
$('#task-search').addEventListener('input', renderTasks);
$('#task-status-filter').addEventListener('change', renderTasks);
$('#task-priority-filter').addEventListener('change', renderTasks);
$('#add-task').addEventListener('click', () => $('#task-dialog').showModal());
$('#drive-folders').addEventListener('click', (event) => { const folder = event.target.closest('[data-folder-id]'); if (folder) loadDrive(folder.dataset.folderId); });
async function uploadDriveFiles(files) { for (const file of files) { const form = new FormData(); form.append('file', file); try { await request(`/api/drive/folders/${state.drive.folderId}/files`, { method: 'POST', body: form }); } catch (error) { showToast(error.message, true); return; } } await loadDrive(state.drive.folderId); showToast('Файлы загружены'); }
$('#drop-zone').addEventListener('click', () => $('#drive-file-input').click());
$('#drive-file-input').addEventListener('change', (event) => uploadDriveFiles(event.target.files));
$('#drop-zone').addEventListener('dragover', (event) => { event.preventDefault(); $('#drop-zone').classList.add('dragging'); });
$('#drop-zone').addEventListener('dragleave', () => $('#drop-zone').classList.remove('dragging'));
$('#drop-zone').addEventListener('drop', (event) => { event.preventDefault(); $('#drop-zone').classList.remove('dragging'); uploadDriveFiles(event.dataTransfer.files); });
$('#create-folder').addEventListener('click', () => $('#folder-dialog').showModal());
$('#folder-form').addEventListener('submit', async (event) => { event.preventDefault(); const formElement = event.currentTarget; const name = new FormData(formElement).get('name'); try { await request('/api/drive/folders', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, parentId: state.drive.folderId }) }); formElement.reset(); $('#folder-dialog').close(); await loadDrive(state.drive.folderId); showToast('Папка создана'); } catch (error) { showToast(error.message, true); } });
$('#manage-folder-access').addEventListener('click', () => { $('#access-subject').innerHTML = state.chats.users.map((user) => `<option value="user:${user.id}">${escapeHTML(user.name)} · ${escapeHTML(user.role)}</option>`).join('') + ['Менеджмент', ...departmentRoles].map((role) => `<option value="group:${escapeHTML(role)}">Группа: ${escapeHTML(role)}</option>`).join(''); $('#access-dialog').showModal(); });
$('#access-form').addEventListener('submit', async (event) => { event.preventDefault(); const form = new FormData(event.currentTarget); const subject = form.get('subject'); const payload = { permission: form.get('permission') }; if (subject.startsWith('user:')) payload.userId = Number(subject.slice(5)); else payload.role = subject.slice(6); try { await request(`/api/drive/folders/${state.drive.folderId}/access`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) }); $('#access-dialog').close(); showToast('Доступ к папке обновлён'); } catch (error) { showToast(error.message, true); } });
$('#chats-list').addEventListener('click', (event) => { const item = event.target.closest('[data-chat-id]'); if (item) selectChat(item.dataset.chatId); });
$('#create-chat').addEventListener('click', () => $('#chat-dialog').showModal());
$('#manage-chat-members').addEventListener('click', () => { if (!state.activeChat) { showToast('Сначала выберите чат', true); return; } $('#chat-member-dialog').showModal(); });
$('#chat-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const form = new FormData(formElement);
  try {
    const chat = await request('/api/chats', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: form.get('name'), memberIds: form.getAll('members').map(Number) }),
    });
    formElement.reset();
    $('#chat-dialog').close();
    await loadChats();
    if (chat.id) await selectChat(chat.id);
    showToast('Чат создан');
  } catch (error) {
    showToast(error.message, true);
  }
});
$('#chat-member-form').addEventListener('submit', async (event) => { event.preventDefault(); if (!state.activeChat) return; const formElement = event.currentTarget; const userId = Number(new FormData(formElement).get('userId')); try { await request(`/api/chats/${state.activeChat.id}/members`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ userId }) }); formElement.reset(); $('#chat-member-dialog').close(); showToast('Участник добавлен'); } catch (error) { showToast(error.message, true); } });
document.querySelectorAll('[data-open-contact]').forEach((button) => button.addEventListener('click', () => $('#contact-dialog').showModal()));
document.querySelectorAll('[data-open-file]').forEach((button) => button.addEventListener('click', () => $('#file-dialog').showModal()));
$('#refresh-contacts').addEventListener('click', () => loadData());
$('#login-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const payload = Object.fromEntries(new FormData(formElement).entries());
  try {
    const user = await request('/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    formElement.reset();
    showAuthenticated(user);
  } catch (error) { showLogin(error.message); }
});
$('#logout').addEventListener('click', async () => { await request('/api/auth/logout', { method: 'POST' }); state.user = null; showLogin(); });
document.querySelectorAll('[data-view]').forEach((item) => item.addEventListener('click', () => setView(item.dataset.view)));
$('#role-content').addEventListener('click', (event) => {
  const action = event.target.closest('[data-role-action]');
  if (!action) return;
  if (action.dataset.roleAction === 'vulnerabilities') {
    $('#vulnerability-workspace').scrollIntoView({ behavior: 'smooth', block: 'center' });
    return;
  }
  setView(action.dataset.roleAction);
});
$('#department-search').addEventListener('input', renderDepartmentItems);
$('#department-status-filter').addEventListener('change', renderDepartmentItems);
$('#refresh-department').addEventListener('click', loadDepartmentItems);
$('#add-department-item').addEventListener('click', () => {
  const isManager = state.user?.isAdmin || state.user?.role === 'Менеджмент';
  if (isManager) $('#department-role').value = departmentRoles[0];
  renderDepartmentKinds(isManager ? $('#department-role').value : state.user.role);
  $('#department-dialog').showModal();
});

$('#department-role').addEventListener('change', (event) => renderDepartmentKinds(event.target.value));
$('#department-items').addEventListener('click', async (event) => {
  const button = event.target.closest('[data-department-status]');
  if (!button) return;
  try {
    await request(`/api/department/items/${button.dataset.departmentStatus}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status: button.dataset.statusValue }) });
    await loadDepartmentItems();
    showToast('Статус рабочей записи обновлён');
  } catch (error) { showToast(error.message, true); }
});
$('#department-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const form = new FormData(formElement);
  const isManager = state.user?.isAdmin || state.user?.role === 'Менеджмент';
  const payload = Object.fromEntries(form.entries());
  if (!isManager) payload.role = state.user.role;
  payload.amount = Number(payload.amount || 0);
  try {
    await request('/api/department/items', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    formElement.reset();
    $('#department-dialog').close();
    await loadDepartmentItems();
    showToast('Рабочая запись создана');
  } catch (error) { showToast(error.message, true); }
});
document.addEventListener('click', (event) => {
  const closeButton = event.target.closest('[data-dialog-close]');
  if (closeButton) {
    const dialog = closeButton.closest('dialog');
    dialog?.querySelector('form')?.reset();
    dialog?.close();
    return;
  }
  const contact = event.target.closest('[data-contact-open]');
  if (!contact) return;
  const item = state.contacts.find((candidate) => candidate.id === Number(contact.dataset.contactOpen));
  if (!item) {
    showToast('Контакт не найден. Обновите список.', true);
    return;
  }
  $('#contact-details-name').textContent = item.name;
  $('#contact-details-company').textContent = item.company || '—';
  $('#contact-details-email').textContent = item.email || '—';
  $('#contact-details-status').textContent = statusLabel(item.status);
  $('#contact-details-value').textContent = money.format(item.value);
  $('#contact-details-created').textContent = formatDate(item.createdAt);
  $('#contact-details-dialog').showModal();
});

document.querySelectorAll('.icon-button').forEach((button) => button.addEventListener('click', () => showToast('Новых уведомлений нет')));
document.querySelectorAll('.avatar-small').forEach((button) => button.addEventListener('click', () => showToast(`${state.user?.name || 'Профиль'} · ${state.user?.role || ''}`)));
document.querySelectorAll('.activity-panel .more').forEach((button) => button.addEventListener('click', () => showToast('Активность обновлена')));
document.querySelectorAll('.channel-item').forEach((button) => button.addEventListener('click', () => { document.querySelectorAll('.channel-item').forEach((item) => item.classList.remove('active')); button.classList.add('active'); showToast(`Открыт канал: ${button.textContent.trim()}`); }));
$('#tasks-list').addEventListener('click', async (event) => {
  const claim = event.target.closest('[data-task-claim]');
  const statusButton = event.target.closest('[data-task-status]');
  if (!claim && !statusButton) return;
  try {
    if (claim) await request(`/api/tasks/${claim.dataset.taskClaim}/claim`, { method: 'POST' });
    if (statusButton) await request(`/api/tasks/${statusButton.dataset.taskStatus}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status: statusButton.dataset.statusValue }) });
    await loadData();
    setView('tasks');
    showToast(claim ? 'Задача взята в работу' : 'Статус задачи обновлён');
  } catch (error) { showToast(error.message, true); }
});
$('#add-vulnerability').addEventListener('click', () => $('#vulnerability-dialog').showModal());
$('#vulnerability-list').addEventListener('change', async (event) => {
  const select = event.target.closest('[data-vulnerability-status]');
  if (!select) return;
  try { await request(`/api/security/vulnerabilities/${select.dataset.vulnerabilityStatus}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ status: select.value }) }); await loadVulnerabilities(); showToast('Статус уязвимости обновлён'); } catch (error) { showToast(error.message, true); }
});
$('#vulnerability-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const payload = Object.fromEntries(new FormData(formElement).entries());
  try { await request('/api/security/vulnerabilities', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) }); formElement.reset(); $('#vulnerability-dialog').close(); await loadVulnerabilities(); showToast('Уязвимость зарегистрирована'); } catch (error) { showToast(error.message, true); }
});
$('#task-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  // Closing a dialog must never trigger task creation, even if the markup regresses.
  if (event.submitter?.matches('[data-dialog-close], [value="cancel"]')) return;
  const formElement = event.currentTarget;
  const form = new FormData(formElement);
  const assignment = form.get('assignment');
  const payload = { title: form.get('title'), description: form.get('description'), priority: form.get('priority') };
  if (assignment?.startsWith('user:')) payload.assigneeId = Number(assignment.slice(5));
  if (assignment?.startsWith('group:')) payload.groupRole = assignment.slice(6);
  try { await request('/api/tasks', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) }); formElement.reset(); $('#task-dialog').close(); await loadData(); setView('tasks'); showToast('Задача создана'); } catch (error) { showToast(error.message, true); }
});
$('#files-list').addEventListener('click', async (event) => {
  const button = event.target.closest('[data-file-open]');
  if (!button) return;
  try {
    const file = await request(`/api/drive/files/${button.dataset.fileOpen}`);
    state.selectedFile = file;
    $('#viewer-kind').textContent = `${file.kind} · ${file.owner || 'Команда'}`;
    $('#viewer-title').textContent = file.name;
    $('#viewer-meta').textContent = `${file.size || 'размер не указан'} · изменён ${formatDate(file.updatedAt)}`;
    $('#file-content').value = file.content || '';
    $('#download-file').href = `/api/drive/files/${file.id}/download`;
    $('#download-file').download = file.name;
    $('#file-viewer').showModal();
  } catch (error) {
    showToast(error.message, true);
  }
});
$('#files-full-list').addEventListener('click', async (event) => {
  const button = event.target.closest('[data-file-open]');
  if (!button) return;
  try {
    const file = await request(`/api/drive/files/${button.dataset.fileOpen}`);
    state.selectedFile = file;
    $('#viewer-kind').textContent = `${file.kind} · ${file.owner || 'Команда'}`;
    $('#viewer-title').textContent = file.name;
    $('#viewer-meta').textContent = `${file.size || 'размер не указан'} · изменён ${formatDate(file.updatedAt)}`;
    $('#file-content').value = file.content || '';
    $('#download-file').href = `/api/drive/files/${file.id}/download`;
    $('#download-file').download = file.name;
    $('#file-viewer').showModal();
  } catch (error) { showToast(error.message, true); }
});

$('#file-viewer-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (!state.selectedFile) return;
  try {
    await request(`/api/drive/files/${state.selectedFile.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: $('#viewer-title').textContent, content: $('#file-content').value }),
    });
    $('#file-viewer').close();
    state.files = await request('/api/workspace/files');
    renderFiles();
    showToast('Файл сохранён');
  } catch (error) {
    showToast(error.message, true);
  }
});
$('#admin-users-list').addEventListener('click', async (event) => {
  const button = event.target.closest('[data-user-save]');
  if (!button) return;
  const id = button.dataset.userSave;
  const role = document.querySelector(`[data-user-role="${id}"]`).value;
  const status = document.querySelector(`[data-user-status="${id}"]`).value;
  try {
    await request(`/api/admin/users/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ role, status }) });
    await loadData();
    showToast('Права пользователя обновлены');
  } catch (error) {
    showToast(error.message, true);
  }
});

$('#message-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const input = event.currentTarget.elements.text;
  try {
    await request('/api/workspace/messages', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ author: state.user.name, role: roles[state.role].label, text: input.value }) });
    input.value = '';
    const messages = await request('/api/workspace/messages');
    state.messages = messages;
    renderMessages();
    showToast('Сообщение отправлено');
  } catch (error) {
    showToast(error.message, true);
  }
});

$('#message-full-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const input = event.currentTarget.elements.text;
  if (!state.activeChat) { showToast('Сначала выберите чат', true); return; }
  try {
    await request(`/api/chats/${state.activeChat.id}/messages`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: input.value }) });
    input.value = '';
    await selectChat(state.activeChat.id);
    showToast('Сообщение отправлено');
  } catch (error) { showToast(error.message, true); }
});

$('#file-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const form = new FormData(formElement);
  const selectedFile = formElement.elements.file.files[0];
  const isDrive = state.view === 'drive';
  let endpoint = isDrive ? `/api/drive/folders/${state.drive.folderId}/files` : '/api/workspace/files';
  let options;
  if (selectedFile) {
    form.delete('name');
    form.delete('content');
    if (!isDrive) endpoint = '/api/workspace/files/upload';
    options = { method: 'POST', body: form };
  } else {
    const payload = Object.fromEntries(form.entries());
    if (!payload.name.trim()) {
      showToast('Укажите название файла или выберите файл', true);
      return;
    }
    endpoint = isDrive ? `/api/drive/folders/${state.drive.folderId}/documents` : endpoint;
    options = { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) };
  }
  if (!selectedFile && !isDrive) options.body = JSON.stringify({ ...JSON.parse(options.body), owner: roles[state.role].label });
  else if (selectedFile && !isDrive) form.set('owner', roles[state.role].label);
  try {
    await request(endpoint, options);
    formElement.reset();
    $('#file-dialog').close();
    if (isDrive) {
      await loadDrive(state.drive.folderId);
    } else {
      state.files = await request('/api/workspace/files');
      renderFiles();
    }
    showToast('Файл добавлен на общий диск');
  } catch (error) {
    showToast(error.message, true);
  }
});

$('#user-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const payload = Object.fromEntries(new FormData(formElement).entries());
  try {
    await request('/api/admin/users', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    formElement.reset();
    $('#user-dialog').close();
    await loadData();
    setView('admin');
    showToast('Пользователь создан');
  } catch (error) {
    showToast(error.message, true);
  }
});

$('#contact-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const formElement = event.currentTarget;
  const form = new FormData(formElement);
  const payload = Object.fromEntries(form.entries());
  payload.value = Number(payload.value || 0);
  try {
    await request('/api/contacts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    formElement.reset();
    $('#contact-dialog').close();
    await loadData();
    showToast('Контакт добавлен');
  } catch (error) {
    showToast(error.message, true);
  }
});

startAuth();
