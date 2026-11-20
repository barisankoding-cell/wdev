// wdev Dashboard Application
document.addEventListener('DOMContentLoaded', () => {
  initModalDialog();
  initServiceConfigDialogs();
  setupNavigation();
  setupMobileDrawer();
  setupSSE();
  loadStatus();
  loadSites();
  loadSwitchers();
  loadCloudflare();
  loadPDNS();
  loadMail();
  loadToolchains();
  setInterval(loadStatus, 4000);
});

// ==========================================
// SYSTEM MODAL ALERT / CONFIRM / PROMPT
// ==========================================
let currentModalResolve = null;
let currentModalMode = 'alert'; // 'alert' | 'confirm' | 'prompt'
let currentModalIsPassword = false;
let isPrivilegedActionPending = false;

function resolveActiveModal(confirmed) {
  const dialog = document.getElementById('modal-alert-dialog');
  const promptInput = document.getElementById('modal-alert-prompt-input');
  if (dialog && dialog.open) {
    dialog.close();
  }
  const resolver = currentModalResolve;
  const mode = currentModalMode;
  const isPass = currentModalIsPassword;
  currentModalResolve = null;
  currentModalIsPassword = false;

  if (!resolver) return;
  if (mode === 'prompt') {
    if (!confirmed) {
      resolver(null);
    } else {
      const raw = promptInput ? promptInput.value : '';
      resolver(isPass ? raw : raw.trim());
    }
  } else if (mode === 'confirm') {
    resolver(Boolean(confirmed));
  } else {
    resolver(true);
  }
}

function resetPendingModalIfOpen() {
  const dialog = document.getElementById('modal-alert-dialog');
  if (currentModalResolve || (dialog && dialog.open)) {
    resolveActiveModal(false);
  }
}

const MODAL_ICONS = {
  info: `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>`,
  success: `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>`,
  warning: `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`,
  error: `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`,
  danger: `<svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`
};

function initModalDialog() {
  const dialog = document.getElementById('modal-alert-dialog');
  if (!dialog) return;

  const cancelBtn = document.getElementById('modal-alert-cancel-btn');
  const confirmBtn = document.getElementById('modal-alert-confirm-btn');
  const promptInput = document.getElementById('modal-alert-prompt-input');

  confirmBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    resolveActiveModal(true);
  });

  cancelBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    resolveActiveModal(false);
  });

  // Handle native Cancel / Escape
  dialog.addEventListener('cancel', (e) => {
    e.preventDefault();
    resolveActiveModal(false);
  });

  // Light dismiss: hanya tutup bila klik tepat pada backdrop <dialog> (bukan synthetic click dari tombol)
  dialog.addEventListener('click', (e) => {
    if (e.target === dialog) {
      resolveActiveModal(false);
    }
  });

  // Allow Enter key in prompt input tanpa memicu click bubbling atau submit berulang
  promptInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      e.stopPropagation();
      resolveActiveModal(true);
    }
  });
}

function modalAlert(message, type = 'info', title = '') {
  resetPendingModalIfOpen();
  return new Promise((resolve) => {
    currentModalMode = 'alert';
    currentModalIsPassword = false;
    currentModalResolve = resolve;

    const dialog = document.getElementById('modal-alert-dialog');
    const titleEl = document.getElementById('modal-alert-title');
    const bodyEl = document.getElementById('modal-alert-body');
    const iconWrapper = document.getElementById('modal-alert-icon-wrapper');
    const promptContainer = document.getElementById('modal-alert-prompt-container');
    const cancelBtn = document.getElementById('modal-alert-cancel-btn');
    const confirmBtn = document.getElementById('modal-alert-confirm-btn');

    const cleanType = MODAL_ICONS[type] ? type : 'info';
    iconWrapper.className = `system-dialog-icon-wrapper ${cleanType}`;
    iconWrapper.innerHTML = MODAL_ICONS[cleanType];

    const defaultTitle = cleanType === 'error' || cleanType === 'danger' ? 'Terjadi Kesalahan' :
                         cleanType === 'warning' ? 'Perhatian' :
                         cleanType === 'success' ? 'Berhasil' : 'Pemberitahuan';
    titleEl.textContent = title || defaultTitle;
    bodyEl.textContent = message;

    promptContainer.style.display = 'none';
    cancelBtn.style.display = 'none';
    confirmBtn.className = 'btn btn-primary';
    confirmBtn.textContent = 'OK';

    dialog.showModal();
    confirmBtn.focus();
  });
}

function modalConfirm(message, options = {}) {
  resetPendingModalIfOpen();
  return new Promise((resolve) => {
    currentModalMode = 'confirm';
    currentModalIsPassword = false;
    currentModalResolve = resolve;

    const {
      title = 'Konfirmasi Tindakan',
      type = 'warning',
      confirmText = 'Lanjutkan',
      cancelText = 'Batal'
    } = (typeof options === 'string') ? { title: options } : options;

    const dialog = document.getElementById('modal-alert-dialog');
    const titleEl = document.getElementById('modal-alert-title');
    const bodyEl = document.getElementById('modal-alert-body');
    const iconWrapper = document.getElementById('modal-alert-icon-wrapper');
    const promptContainer = document.getElementById('modal-alert-prompt-container');
    const cancelBtn = document.getElementById('modal-alert-cancel-btn');
    const confirmBtn = document.getElementById('modal-alert-confirm-btn');

    const cleanType = MODAL_ICONS[type] ? type : 'warning';
    iconWrapper.className = `system-dialog-icon-wrapper ${cleanType}`;
    iconWrapper.innerHTML = MODAL_ICONS[cleanType];

    titleEl.textContent = title;
    bodyEl.textContent = message;

    promptContainer.style.display = 'none';
    cancelBtn.style.display = 'inline-flex';
    cancelBtn.textContent = cancelText;

    confirmBtn.className = (cleanType === 'danger' || cleanType === 'error') ? 'btn btn-danger' : 'btn btn-primary';
    confirmBtn.textContent = confirmText;

    dialog.showModal();
    cancelBtn.focus(); // Autofocus safe action per accessibility guidelines
  });
}

function modalPrompt(message, options = {}) {
  resetPendingModalIfOpen();
  return new Promise((resolve) => {
    const {
      title = 'Input Data',
      placeholder = '',
      defaultValue = '',
      confirmText = 'Simpan',
      cancelText = 'Batal',
      isPassword = false
    } = (typeof options === 'string') ? { title: options } : options;

    currentModalMode = 'prompt';
    currentModalIsPassword = Boolean(isPassword);
    currentModalResolve = resolve;

    const dialog = document.getElementById('modal-alert-dialog');
    const titleEl = document.getElementById('modal-alert-title');
    const bodyEl = document.getElementById('modal-alert-body');
    const iconWrapper = document.getElementById('modal-alert-icon-wrapper');
    const promptContainer = document.getElementById('modal-alert-prompt-container');
    const promptInput = document.getElementById('modal-alert-prompt-input');
    const cancelBtn = document.getElementById('modal-alert-cancel-btn');
    const confirmBtn = document.getElementById('modal-alert-confirm-btn');

    iconWrapper.className = 'system-dialog-icon-wrapper info';
    iconWrapper.innerHTML = MODAL_ICONS.info;

    titleEl.textContent = title;
    bodyEl.textContent = message;

    promptContainer.style.display = 'block';
    promptInput.type = isPassword ? 'password' : 'text';
    promptInput.value = defaultValue;
    promptInput.placeholder = placeholder;
    promptInput.autocomplete = isPassword ? 'current-password' : 'off';

    cancelBtn.style.display = 'inline-flex';
    cancelBtn.textContent = cancelText;

    confirmBtn.className = 'btn btn-primary';
    confirmBtn.textContent = confirmText;

    dialog.showModal();
    setTimeout(() => {
      promptInput.focus();
      promptInput.select();
    }, 50);
  });
}

// Override standard browser alert, confirm, and prompt
window.alert = (msg) => modalAlert(msg);
window.confirm = (msg) => modalConfirm(msg);
window.prompt = (msg, def) => modalPrompt(msg, { defaultValue: def });

// Mobile Drawer
function setupMobileDrawer() {
  const toggleBtn = document.getElementById('mobile-menu-toggle');
  const closeBtn = document.getElementById('sidebar-close');
  const backdrop = document.getElementById('sidebar-backdrop');
  const sidebar = document.getElementById('app-sidebar');

  function openSidebar() {
    sidebar.classList.add('open');
    backdrop.classList.add('active');
  }

  function closeSidebar() {
    sidebar.classList.remove('open');
    backdrop.classList.remove('active');
  }

  if (toggleBtn) toggleBtn.addEventListener('click', openSidebar);
  if (closeBtn) closeBtn.addEventListener('click', closeSidebar);
  if (backdrop) backdrop.addEventListener('click', closeSidebar);

  // Auto-close drawer on mobile when selecting a navigation tab
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', () => {
      if (window.innerWidth <= 820) {
        closeSidebar();
      }
    });
  });
}

// Navigation Tabs
function setupNavigation() {
  const items = document.querySelectorAll('.nav-item');
  items.forEach(item => {
    item.addEventListener('click', () => {
      items.forEach(i => i.classList.remove('active'));
      item.classList.add('active');
      const tabName = item.getAttribute('data-tab');
      document.querySelectorAll('.tab-content').forEach(tc => tc.classList.remove('active'));
      const activeTab = document.getElementById('tab-' + tabName);
      if (activeTab) activeTab.classList.add('active');

      const titleMap = {
        'status': 'Status & Pembaruan Sistem',
        'webstack': 'Web Stack & Domain Lokal (wdev site)',
        'switchers': 'Pengelola Multi-Versi Runtime (Switchers)',
        'toolchains': 'Toolchains, Compilers & SDKs',
        'workspace': 'Workspace, ADE & Shell',
        'powerdns': 'PowerDNS & VPS Edge Gateway (Netbird)',
        'cloudflare': 'Cloudflare Tunnel & DNS Manager',
        'mail': 'Mail Server & Outbound Relay per Domain',
        'full-install': 'Instalasi Lengkap (Full Auto Setup)',
        'modular': 'Instalasi Komponen Modular'
      };
      document.getElementById('topbar-page-title').textContent = titleMap[tabName] || 'Dashboard';
      if (tabName === 'webstack') {
        loadSites();
      }
      if (tabName === 'switchers') {
        loadSwitchers();
      }
      if (tabName === 'toolchains') {
        loadToolchains();
      }
      if (tabName === 'workspace') {
        loadWorkspace();
      }
      if (tabName === 'powerdns') {
        loadPDNS();
      }
      if (tabName === 'cloudflare') {
        loadCloudflare();
      }
      if (tabName === 'mail') {
        loadMail();
      }
    });
  });
}

// System Status Polling
async function loadStatus() {
  try {
    const res = await fetch('/api/status');
    if (!res.ok) return;
    const data = await res.json();

    // Top metrics
    document.getElementById('stat-os').textContent = data.os_name;
    document.getElementById('stat-uptime').textContent = data.uptime_string;
    document.getElementById('stat-load').textContent = data.load_avg;
    document.getElementById('stat-mem-val').textContent = `${data.mem_used_mb} / ${data.mem_total_mb} MB (${data.mem_percent}%)`;
    document.getElementById('stat-mem-bar').style.width = `${data.mem_percent}%`;
    document.getElementById('stat-disk-val').textContent = `${data.disk_used_gb} / ${data.disk_total_gb} GB (${data.disk_percent}%)`;
    document.getElementById('stat-disk-bar').style.width = `${data.disk_percent}%`;
    const lanEl = document.getElementById('stat-lan-ip');
    if (lanEl) lanEl.textContent = data.lan_ip;
    const nbEl = document.getElementById('stat-netbird-ip');
    if (nbEl) nbEl.textContent = data.netbird_ip;

    const cardNb = document.getElementById('stat-card-netbird-ip');
    if (cardNb) cardNb.textContent = data.netbird_ip;
    const cardLan = document.getElementById('stat-card-lan-ip');
    if (cardLan) cardLan.textContent = data.lan_ip;

    // Services
    renderServices(data.services);

    // Runtimes
    renderRuntimes(data.runtimes);
  } catch (err) {
    console.error('Failed to load status:', err);
  }
}

function switchToTab(tabName) {
  const btn = document.querySelector(`[data-tab="${tabName}"]`);
  if (btn) btn.click();
}

let isRefreshingServices = false;

async function refreshCoreServices(forceRefresh = true) {
  if (isRefreshingServices) return;
  const btn = document.getElementById('btn-refresh-services');
  const btnText = document.getElementById('btn-refresh-services-text');

  if (forceRefresh && btn) {
    isRefreshingServices = true;
    if (btnText) btnText.textContent = 'Memeriksa...';
    btn.disabled = true;
  }

  try {
    const url = forceRefresh ? '/api/status?refresh=1' : '/api/status';
    const res = await fetch(url);
    if (!res.ok) return;
    const data = await res.json();
    renderServices(data.services);
    renderRuntimes(data.runtimes);
  } catch (err) {
    console.error('Failed to refresh core services:', err);
    if (forceRefresh) {
      await modalAlert('Gagal memeriksa pembaruan layanan: ' + err.message, 'error', 'Layanan Inti');
    }
  } finally {
    isRefreshingServices = false;
    if (btn) {
      btn.disabled = false;
      if (btnText) btnText.textContent = 'Periksa Pembaruan';
    }
  }
}

function renderServices(services) {
  const container = document.getElementById('services-container');
  if (!container || !services) return;
  container.innerHTML = '';

  services.forEach(s => {
    const card = document.createElement('div');
    card.className = `service-card ${s.has_update ? 'has-update' : ''}`;
    card.setAttribute('data-service', s.name);

    // Status Badge & Action Buttons
    let statusPill = '';
    let actionsHtml = '';

    if (!s.installed) {
      statusPill = `<span class="status-pill stopped">Belum Terpasang</span>`;
      if (s.name === 'stalwart') {
        actionsHtml = `
          <button class="btn btn-sm btn-primary" onclick="switchToTab('mail')" style="display:inline-flex;align-items:center;gap:6px;width:100%;">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            <span>Pasang Stalwart Mail</span>
          </button>
        `;
      } else if (s.name === 'pdns') {
        actionsHtml = `
          <button class="btn btn-sm btn-primary" onclick="switchToTab('powerdns')" style="display:inline-flex;align-items:center;gap:6px;width:100%;">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            <span>Setup PowerDNS / Edge</span>
          </button>
        `;
      } else {
        const cleanName = escapeHtml((s.display_name || '').split(' (')[0]);
        actionsHtml = `
          <button class="btn btn-sm btn-primary" onclick="triggerInstall('${s.install_component}')" style="display:inline-flex;align-items:center;gap:6px;width:100%;">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            <span>Pasang ${cleanName}</span>
          </button>
        `;
      }
    } else if (s.has_update) {
      statusPill = `<span class="status-pill warning">★ Update Tersedia</span>`;
      let controlBtn = '';
      if (s.active) {
        controlBtn = `<button class="btn btn-sm btn-outline" onclick="serviceAction('${s.name}', 'restart')" title="Restart Layanan" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg> Restart</button>`;
      } else {
        controlBtn = `<button class="btn btn-sm btn-outline" onclick="serviceAction('${s.name}', 'start')" title="Jalankan Layanan" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg> Jalankan</button>`;
      }
      const configBtn = (s.name === 'redis' || s.name === 'postgresql' || s.name === 'php-fpm' || s.name === 'nginx')
        ? `<button type="button" class="btn btn-sm btn-outline" onclick="openServiceConfigModal('${s.name}')" title="Pengaturan ${escapeHtml(s.display_name)}" aria-label="Konfigurasi ${escapeHtml(s.display_name)}" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg> Konfigurasi</button>`
        : '';

      actionsHtml = `
        <button class="btn btn-sm btn-warning" onclick="triggerInstall('${s.install_component}')" title="Perbarui ke versi ${escapeHtml(s.latest_version)}" style="display:inline-flex;align-items:center;gap:6px;flex:1;">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
          <span>Perbarui ke ${escapeHtml(s.latest_version)}</span>
        </button>
        ${controlBtn}
        ${configBtn}
      `;
    } else {
      // Installed & up-to-date
      const configBtn = (s.name === 'redis' || s.name === 'postgresql' || s.name === 'php-fpm' || s.name === 'nginx')
        ? `<button type="button" class="btn btn-sm btn-outline" onclick="openServiceConfigModal('${s.name}')" title="Pengaturan ${escapeHtml(s.display_name)}" aria-label="Konfigurasi ${escapeHtml(s.display_name)}" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg> Konfigurasi</button>`
        : '';

      if (s.is_remote) {
        statusPill = `<span class="status-pill ${s.active ? 'active' : 'stopped'}">${s.active ? '● Online (Edge VPS)' : '○ Nonaktif'}</span>`;
        if (s.active) {
          actionsHtml = `
            <button class="btn btn-sm btn-primary" onclick="switchToTab('powerdns')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/></svg> Kelola DNS</button>
            <button class="btn btn-sm btn-outline" onclick="serviceAction('${s.name}', 'restart')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg> Tes Koneksi</button>
          `;
        } else {
          actionsHtml = `
            <button class="btn btn-sm btn-primary" onclick="switchToTab('powerdns')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg> Cek Konfigurasi</button>
            <button class="btn btn-sm btn-outline" onclick="serviceAction('${s.name}', 'restart')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg> Coba Hubungkan</button>
          `;
        }
      } else {
        statusPill = `<span class="status-pill ${s.active ? 'active' : 'stopped'}">${s.active ? '● Aktif' : '○ Nonaktif'}</span>`;
        if (s.active) {
          actionsHtml = `
            <button class="btn btn-sm btn-outline" onclick="serviceAction('${s.name}', 'restart')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg> Restart</button>
            ${configBtn}
            <button class="btn btn-sm btn-danger" onclick="serviceAction('${s.name}', 'stop')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="6" width="12" height="12" rx="2"/></svg> Hentikan</button>
          `;
        } else {
          actionsHtml = `
            <button class="btn btn-sm btn-primary" onclick="serviceAction('${s.name}', 'start')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg> Jalankan</button>
            ${configBtn}
            <button class="btn btn-sm btn-outline" onclick="triggerInstall('${s.install_component}')" style="display:inline-flex;align-items:center;gap:4px;" title="Pasang ulang atau perbarui paket"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Pasang Ulang</button>
          `;
        }
      }
    }

    // Version box display
    let versionRow = '';
    if (!s.installed) {
      versionRow = `
        <div class="toolchain-version-box" style="margin-top:8px;padding:8px 10px;">
          <div class="toolchain-version-row">
            <span class="label">Versi Terpasang:</span>
            <span class="val" style="color:var(--text-muted);font-style:italic;">Belum terpasang</span>
          </div>
          <div class="toolchain-version-row divider">
            <span class="label">Rilis Terbaru:</span>
            <span class="val">${escapeHtml(s.latest_version || '-')}</span>
          </div>
        </div>
      `;
    } else {
      const verText = s.version || 'Terpasang';
      const latestText = s.has_update
        ? `<span style="color:var(--warning);font-weight:700;">${escapeHtml(s.latest_version)} <span style="font-size:10px;padding:1px 4px;background:rgba(245,158,11,0.15);border-radius:3px;">Pembaruan</span></span>`
        : `<span>${escapeHtml(s.latest_version || verText)}</span>`;

      versionRow = `
        <div class="toolchain-version-box" style="margin-top:8px;padding:8px 10px;">
          <div class="toolchain-version-row">
            <span class="label">Versi Terpasang:</span>
            <span class="val" style="color:var(--text-main);font-weight:700;">${escapeHtml(verText)}</span>
          </div>
          <div class="toolchain-version-row divider">
            <span class="label">Rilis Resmi:</span>
            <span class="val">${latestText}</span>
          </div>
        </div>
      `;
    }

    card.innerHTML = `
      <div>
        <div class="service-info">
          <div>
            <div class="service-name">
              <span>${escapeHtml(s.display_name)}</span>
            </div>
            <div class="service-meta" style="margin-top:2px;">${escapeHtml(s.category)} ${s.port ? '• ' + escapeHtml(s.port) : ''}</div>
          </div>
          <div>
            ${statusPill}
          </div>
        </div>
        ${versionRow}
      </div>
      <div class="service-actions" style="margin-top:12px;">
        ${actionsHtml}
      </div>
    `;
    container.appendChild(card);
  });
}

function renderRuntimes(runtimes) {
  const container = document.getElementById('runtimes-container');
  if (!container || !runtimes) return;
  container.innerHTML = '';

  for (const [name, ver] of Object.entries(runtimes)) {
    const badge = document.createElement('div');
    badge.className = 'stat-badge stat-badge-clickable';
    badge.title = `Klik untuk beralih / kelola versi ${name.toUpperCase()} di Multi-Versi Runtime`;
    badge.innerHTML = `<strong>${name.toUpperCase()}:</strong> <span>${escapeHtml(ver)}</span> <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-left:4px;opacity:0.6;"><path d="M9 18l6-6-6-6"/></svg>`;
    badge.onclick = () => switchToTab('switchers');
    container.appendChild(badge);
  }
}

async function serviceAction(name, action) {
  try {
    const res = await fetch(`/api/service/${name}/${action}`, { method: 'POST' });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal: ${data.error || 'Terjadi kesalahan'}`, 'error', 'Kontrol Layanan');
    }
    loadStatus();
  } catch (err) {
    await modalAlert('Koneksi gagal: ' + err, 'error', 'Kesalahan Jaringan');
  }
}

function escapeHtml(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// ==========================================
// TOOLCHAINS, COMPILERS & SDKS
// ==========================================
let cachedToolchains = null;
let isLoadingToolchains = false;

async function loadToolchains(forceRefresh = false) {
  if (isLoadingToolchains && forceRefresh) return;
  const refreshBtn = document.getElementById('btn-refresh-toolchains');
  const refreshText = document.getElementById('btn-refresh-toolchains-text');

  if (forceRefresh && refreshBtn) {
    isLoadingToolchains = true;
    if (refreshText) refreshText.textContent = 'Memeriksa...';
    refreshBtn.disabled = true;
  }

  try {
    const url = forceRefresh ? '/api/toolchains?refresh=1' : '/api/toolchains';
    const res = await fetch(url);
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    const items = await res.json();
    cachedToolchains = items;
    renderToolchains(items);
  } catch (err) {
    console.error('Failed to load toolchains:', err);
    if (forceRefresh) {
      await modalAlert('Gagal memeriksa pembaruan toolchain: ' + err.message, 'error', 'Toolchain SDK');
    }
  } finally {
    isLoadingToolchains = false;
    if (refreshBtn) {
      refreshBtn.disabled = false;
      if (refreshText) refreshText.textContent = 'Periksa Pembaruan';
    }
  }
}

function renderToolchains(items) {
  const container = document.getElementById('toolchains-container');
  if (!container || !Array.isArray(items)) return;

  container.innerHTML = '';

  items.forEach(item => {
    const card = document.createElement('div');
    card.className = `service-card toolchain-card ${item.has_update ? 'has-update' : ''}`;
    card.setAttribute('data-id', item.id);

    // Status Badge & Action Button
    let statusBadge = '';
    let actionBtn = '';

    if (!item.installed) {
      statusBadge = `<span class="status-pill stopped">Belum Terpasang</span>`;
      actionBtn = `
        <button type="button" class="btn btn-sm btn-primary" style="width:100%;" onclick="triggerInstall('${item.install_component}')">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          <span>Pasang Sekarang</span>
        </button>
      `;
    } else if (item.has_update) {
      statusBadge = `<span class="status-pill warning">★ Update Tersedia</span>`;
      actionBtn = `
        <button type="button" class="btn btn-sm btn-warning" style="width:100%;" onclick="triggerInstall('${item.install_component}')" title="Perbarui ke versi ${escapeHtml(item.latest_version)}">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
          <span>Perbarui ke ${escapeHtml(item.latest_version)}</span>
        </button>
      `;
    } else {
      statusBadge = `<span class="status-pill active">✓ Terpasang</span>`;
      actionBtn = `
        <button type="button" class="btn btn-sm btn-outline" style="width:100%;" onclick="triggerInstall('${item.install_component}')" title="Toolchain sudah up-to-date. Klik untuk pasang ulang atau sinkronisasi">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          <span>Pasang Ulang / Sinkron</span>
        </button>
      `;
    }

    const installedDisplay = item.installed
      ? `<span style="color:var(--text-main);font-weight:700;">${escapeHtml(item.installed_version)}</span>`
      : `<span style="color:var(--text-muted);font-style:italic;">Belum terpasang</span>`;

    const latestDisplay = item.has_update
      ? `<span style="color:var(--warning);font-weight:700;">${escapeHtml(item.latest_version)} <span style="font-size:10.5px;padding:1px 5px;background:rgba(245,158,11,0.15);border-radius:4px;">Pembaruan</span></span>`
      : `<span>${escapeHtml(item.latest_version || '-')}</span>`;

    card.innerHTML = `
      <div>
        <div class="service-info">
          <div class="service-name">
            <span>${escapeHtml(item.name)}</span>
          </div>
          <div>
            ${statusBadge}
          </div>
        </div>
        <div class="service-meta" style="margin-top:6px;line-height:1.45;">
          ${escapeHtml(item.description)}
        </div>

        <div class="toolchain-version-box">
          <div class="toolchain-version-row">
            <span class="label">Versi Terpasang:</span>
            <span class="val">${installedDisplay}</span>
          </div>
          ${item.extra_info ? `
          <div class="toolchain-version-row divider">
            <span class="label">Detail / Info:</span>
            <span class="val" style="color:var(--text-dim);font-size:11px;">${escapeHtml(item.extra_info)}</span>
          </div>` : ''}
          <div class="toolchain-version-row divider">
            <span class="label">Rilis Resmi Terbaru:</span>
            <span class="val">${latestDisplay}</span>
          </div>
        </div>
      </div>

      <div style="margin-top:14px;">
        ${actionBtn}
      </div>
    `;

    container.appendChild(card);
  });
}

// ==========================================
// Workspace, ADE & Shell
// ==========================================
let cachedWorkspaceData = null;

async function loadWorkspace(forceRefresh = false) {
  const container = document.getElementById('workspace-container');
  if (!container) return;

  try {
    const url = forceRefresh ? '/api/workspace/status?refresh=1' : '/api/workspace/status';
    const res = await fetch(url);
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    const data = await res.json();
    cachedWorkspaceData = data;
    renderWorkspace(data);
  } catch (err) {
    console.error('Failed to load workspace status:', err);
    container.innerHTML = `
      <div class="service-card" style="grid-column: 1 / -1; padding: 20px; text-align: center; color: var(--text-dim);">
        Gagal memuat status Workspace & ADE: ${escapeHtml(err.message)}
      </div>
    `;
  }
}

function renderWorkspace(data) {
  const container = document.getElementById('workspace-container');
  if (!container) return;
  container.innerHTML = '';

  const orca = data.orca || {};
  const shell = data.shell || {};

  // Card 1: Orca ADE
  const orcaCard = document.createElement('div');
  orcaCard.className = `service-card toolchain-card ${orca.has_update ? 'has-update' : ''}`;

  const orcaActive = orca.active;
  const orcaInstalled = orca.installed;
  const orcaMode = (orca.mode || 'headless').toLowerCase();
  const isHeadless = orcaMode === 'headless';

  let orcaStatusBadge = '';
  if (orcaActive) {
    orcaStatusBadge = `<span class="status-pill active"><span class="pulse-dot pulse-success"></span> Running (${isHeadless ? 'Headless Server' : 'Desktop GUI'})</span>`;
  } else if (orcaInstalled) {
    orcaStatusBadge = `<span class="status-pill stopped"><span class="pulse-dot pulse-danger"></span> Stopped</span>`;
  } else {
    orcaStatusBadge = `<span class="status-pill unknown">Belum Terpasang</span>`;
  }
  if (orca.has_update) {
    orcaStatusBadge += ` <span class="status-pill warning">★ Update Tersedia</span>`;
  }

  let orcaActions = '';
  if (orcaInstalled) {
    orcaActions += `
      <div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:14px;">
        ${orca.has_update ? `
          <button type="button" class="btn btn-sm btn-warning" onclick="triggerInstall('orca-update')" title="Perbarui Orca ADE ke v${escapeHtml(orca.latest_version)}" style="display:inline-flex;align-items:center;gap:6px;">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            <span>Perbarui ke v${escapeHtml(orca.latest_version)}</span>
          </button>
        ` : ''}
        ${orca.web_client_url ? `
          <a href="${escapeHtml(orca.web_client_url)}" target="_blank" class="btn btn-sm btn-primary" style="display:inline-flex;align-items:center;gap:6px;">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
            <span>Buka Web Client</span>
          </a>
        ` : ''}
        <button type="button" class="btn btn-sm btn-outline" onclick="openOrcaPairModal()" style="display:inline-flex;align-items:center;gap:6px;">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
          <span>Info & Pairing</span>
        </button>
        ${orcaActive ? `
          <button type="button" class="btn btn-sm btn-outline" onclick="controlOrcaService('restart')" title="Restart layanan orca">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>
            <span>Restart</span>
          </button>
          <button type="button" class="btn btn-sm btn-outline" style="color:var(--danger);" onclick="controlOrcaService('stop')" title="Hentikan layanan orca">
            Stop
          </button>
        ` : `
          <button type="button" class="btn btn-sm btn-success" onclick="controlOrcaService('start')" title="Jalankan layanan orca">
            Mulai Layanan
          </button>
        `}
        ${isHeadless ? `
          <button type="button" class="btn btn-sm btn-outline" onclick="triggerInstall('orca-switch-desktop')" title="Beralih ke mode Desktop GUI">
            Ganti ke Desktop GUI
          </button>
        ` : `
          <button type="button" class="btn btn-sm btn-outline" onclick="triggerInstall('orca-switch-headless')" title="Beralih ke mode Headless Server 24/7">
            Ganti ke Headless
          </button>
        `}
        <button type="button" class="btn btn-sm btn-outline" onclick="triggerInstall('orca-ai')" title="Pengaturan Git Commit AI">
          Commit AI
        </button>
      </div>
    `;
  } else {
    orcaActions += `
      <div style="display:flex;gap:6px;flex-wrap:wrap;margin-top:14px;">
        <button type="button" class="btn btn-sm btn-primary" onclick="triggerInstall('orca-headless')">
          Setup Headless Server
        </button>
        <button type="button" class="btn btn-sm btn-outline" onclick="triggerInstall('orca-desktop')">
          Setup Desktop GUI
        </button>
      </div>
    `;
  }

  orcaCard.innerHTML = `
    <div>
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:8px;">
        <div>
          <div class="service-name" style="font-size:16px;">🐬 Orca ADE</div>
          <div style="font-size:11.5px;color:var(--text-dim);font-weight:600;">Autonomous Development Environment</div>
        </div>
        <div>${orcaStatusBadge}</div>
      </div>
      <div class="service-meta" style="margin-top:8px;line-height:1.45;">
        IDE & Agent Workspace otonom untuk Linux, mendukung eksekusi lokal dan remote web access terenkripsi.
      </div>

      <div class="toolchain-version-box" style="margin-top:12px;">
        <div class="toolchain-version-row">
          <span class="label">Versi Terpasang:</span>
          <span class="val">${orca.version ? 'v' + escapeHtml(orca.version) : (orcaInstalled ? 'Terpasang' : '<span style="color:var(--text-dim);">Belum Terpasang</span>')}</span>
        </div>
        <div class="toolchain-version-row divider">
          <span class="label">Rilis Resmi Terbaru:</span>
          <span class="val">${orca.has_update
            ? `<span style="color:var(--warning);font-weight:700;">v${escapeHtml(orca.latest_version)} <span style="font-size:10.5px;padding:1px 5px;background:rgba(245,158,11,0.15);border-radius:4px;">Pembaruan</span></span>`
            : (orca.latest_version ? 'v' + escapeHtml(orca.latest_version) : '-')}</span>
        </div>
        <div class="toolchain-version-row divider">
          <span class="label">Mode Operasi:</span>
          <span class="val" style="font-weight:600;">${isHeadless ? 'Headless Server 24/7 (systemd)' : 'Desktop GUI (X11/Wayland)'}</span>
        </div>
        ${orcaActive ? `
          <div class="toolchain-version-row divider">
            <span class="label">Port WebSocket:</span>
            <span class="val" style="font-family:monospace;font-weight:600;color:var(--primary);">${orca.port || 6768}</span>
          </div>
          ${orca.pid ? `
            <div class="toolchain-version-row divider">
              <span class="label">PID Proses:</span>
              <span class="val" style="font-family:monospace;">${orca.pid}</span>
            </div>
          ` : ''}
        ` : ''}
      </div>
    </div>
    ${orcaActions}
  `;
  container.appendChild(orcaCard);

  // Card 2: Shell
  const shellCard = document.createElement('div');
  shellCard.className = 'service-card toolchain-card';

  const omzInstalled = shell.oh_my_zsh;
  const currentShell = shell.current_shell || '/bin/bash';
  const activeTheme = shell.active_theme || '-';
  const nerdFont = shell.nerd_font || '';

  shellCard.innerHTML = `
    <div>
      <div style="display:flex;align-items:flex-start;justify-content:space-between;gap:8px;">
        <div>
          <div class="service-name" style="font-size:16px;">🐚 Oh My Zsh & Terminal Shell</div>
          <div style="font-size:11.5px;color:var(--text-dim);font-weight:600;">CLI Developer Productivity & Theme</div>
        </div>
        <div>
          ${omzInstalled ?
            `<span class="status-pill active"><span class="pulse-dot pulse-success"></span> Terpasang</span>` :
            `<span class="status-pill unknown">Belum Terpasang</span>`
          }
        </div>
      </div>
      <div class="service-meta" style="margin-top:8px;line-height:1.45;">
        Shell interaktif Zsh lengkap dengan konfigurasi komunitas, autosuggestions, dan tema prompt modern.
      </div>

      <div class="toolchain-version-box" style="margin-top:12px;">
        <div class="toolchain-version-row">
          <span class="label">Shell Default Pengguna:</span>
          <span class="val" style="font-family:monospace;font-weight:600;">${escapeHtml(currentShell)}</span>
        </div>
        <div class="toolchain-version-row divider">
          <span class="label">Status Oh My Zsh:</span>
          <span class="val">${omzInstalled ? 'Aktif di ~/.oh-my-zsh' : '<span style="color:var(--text-dim);">Belum Terpasang</span>'}</span>
        </div>
        <div class="toolchain-version-row divider">
          <span class="label">Tema Prompt Aktif:</span>
          <span class="val" style="font-weight:600;color:var(--primary);">${escapeHtml(activeTheme)}</span>
        </div>
        <div class="toolchain-version-row divider">
          <span class="label">Nerd Font:</span>
          <span class="val">${nerdFont ? `<span style="color:var(--success);font-weight:600;">${escapeHtml(nerdFont)}</span>` : '<span style="color:var(--text-dim);">Belum Terpasang</span>'}</span>
        </div>
      </div>
    </div>

    <div style="margin-top:14px;">
      <label style="font-size:11px;color:var(--text-dim);font-weight:700;display:block;margin-bottom:6px;text-transform:uppercase;">GANTI TEMA PROMPT:</label>
      <div style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px;">
        <button type="button" class="btn btn-sm ${activeTheme.includes('powerlevel10k') ? 'btn-primary' : 'btn-outline'}" onclick="triggerInstall('zsh-p10k')">
          Powerlevel10k
        </button>
        <button type="button" class="btn btn-sm ${activeTheme.includes('agnoster') ? 'btn-primary' : 'btn-outline'}" onclick="triggerInstall('zsh-agnoster')">
          Agnoster
        </button>
        <button type="button" class="btn btn-sm ${activeTheme.includes('robbyrussell') ? 'btn-primary' : 'btn-outline'}" onclick="triggerInstall('zsh-robbyrussell')">
          Robbyrussell
        </button>
      </div>

      <div style="display:flex;gap:6px;flex-wrap:wrap;">
        <button type="button" class="btn btn-sm ${omzInstalled ? 'btn-outline' : 'btn-primary'}" onclick="triggerInstall('zsh')">
          ${omzInstalled ? 'Pasang Ulang / Sinkron Oh My Zsh' : 'Instal Oh My Zsh Lengkap'}
        </button>
      </div>
    </div>
  `;
  container.appendChild(shellCard);
}

async function controlOrcaService(action) {
  try {
    const res = await fetch(`/api/service/orca/${action}`, { method: 'POST' });
    if (!res.ok) {
      const err = await res.json();
      throw new Error(err.error || 'Aksi gagal');
    }
    await loadWorkspace();
  } catch (err) {
    await modalAlert('Gagal mengontrol layanan Orca: ' + err.message, 'error', 'Orca ADE');
  }
}

function openOrcaPairModal() {
  const modal = document.getElementById('modal-orca-pair');
  if (!modal) return;
  const data = cachedWorkspaceData && cachedWorkspaceData.orca ? cachedWorkspaceData.orca : {};

  const webUrlInput = document.getElementById('orca-modal-weburl');
  const webLink = document.getElementById('orca-modal-weblink');
  const pairCodeInput = document.getElementById('orca-modal-paircode');
  const modeSpan = document.getElementById('orca-modal-mode');
  const portSpan = document.getElementById('orca-modal-port');
  const statusSpan = document.getElementById('orca-modal-status');

  if (webUrlInput) webUrlInput.value = data.web_client_url || `http://${window.location.hostname}:6768`;
  if (webLink) {
    if (data.web_client_url) {
      webLink.href = data.web_client_url;
      webLink.style.display = 'inline-flex';
    } else {
      webLink.style.display = 'none';
    }
  }
  if (pairCodeInput) pairCodeInput.value = data.pairing_url || '-';
  if (modeSpan) modeSpan.textContent = data.mode === 'desktop' ? 'Desktop GUI' : 'Headless Server';
  if (portSpan) portSpan.textContent = (data.port || 6768).toString();
  if (statusSpan) statusSpan.textContent = data.active ? 'Aktif (Running)' : 'Tidak Aktif (Stopped)';

  modal.classList.add('active');
}

function closeOrcaPairModal() {
  const modal = document.getElementById('modal-orca-pair');
  if (modal) modal.classList.remove('active');
}

function copyOrcaWebUrl() {
  const input = document.getElementById('orca-modal-weburl');
  if (input && input.value) {
    navigator.clipboard.writeText(input.value).then(() => {
      modalAlert('URL Web Client berhasil disalin ke clipboard!', 'success', 'Orca ADE');
    }).catch(() => {
      input.select();
      document.execCommand('copy');
      modalAlert('URL Web Client disalin!', 'success', 'Orca ADE');
    });
  }
}

function copyOrcaPairCode() {
  const input = document.getElementById('orca-modal-paircode');
  if (input && input.value) {
    navigator.clipboard.writeText(input.value).then(() => {
      modalAlert('Pairing URI berhasil disalin ke clipboard!', 'success', 'Orca ADE');
    }).catch(() => {
      input.select();
      document.execCommand('copy');
      modalAlert('Pairing URI disalin!', 'success', 'Orca ADE');
    });
  }
}

// Sites
async function loadSites() {
  try {
    const res = await fetch('/api/sites');
    if (!res.ok) return;
    const sites = await res.json() || [];
    const tbody = document.getElementById('sites-table-body');
    if (!tbody) return;
    tbody.innerHTML = '';

    if (sites.length === 0) {
      tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;color:var(--text-dim);padding:24px;">Belum ada virtual host. Klik "+ Tambah Virtual Host" untuk membuatnya.</td></tr>`;
      return;
    }

    sites.forEach(s => {
      const tr = document.createElement('tr');
      const proto = s.ssl_enabled ? 'https://' : 'http://';
      tr.innerHTML = `
        <td><strong><a href="${proto}${s.domain}" target="_blank" style="color:var(--primary);text-decoration:none;">${s.domain}</a></strong></td>
        <td><span class="stat-badge" style="display:inline-block;padding:2px 8px;font-size:11px;">${s.framework.toUpperCase()}</span></td>
        <td>${s.framework === 'proxy' ? `Forward :${s.proxy_port}` : `PHP ${s.php_version}`}</td>
        <td><code style="font-size:11px;color:var(--text-muted);max-width:180px;display:inline-block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:bottom;" title="${s.root_path || '-'}">${s.root_path || '-'}</code></td>
        <td>${s.ssl_enabled ? '<span style="color:var(--success);">✓ SSL</span>' : '<span style="color:var(--text-dim);">HTTP</span>'}</td>
        <td>
          ${s.exposed_to ?
            `<a href="https://${s.exposed_to}" target="_blank" rel="noopener" style="text-decoration:none;"><span class="status-pill active" style="font-size:11px;font-weight:600;">● ${s.exposed_to} ↗</span></a>` :
            `<button class="btn btn-sm btn-outline" onclick="openPDNSExposeModal('site', '${s.domain}')" style="font-size:11px;padding:3px 8px;display:inline-flex;align-items:center;gap:4px;" title="Ekspos vhost ke Edge VPS"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/><path d="M12 13v6"/><path d="m9 16 3-3 3 3"/></svg> Ekspos VPS</button>`}
        </td>
        <td>
          <button class="btn btn-sm btn-danger" onclick="deleteSite('${s.domain}')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg> Hapus</button>
        </td>
      `;
      tbody.appendChild(tr);
    });

    if (typeof loadPDNSLocalSites === 'function') {
      loadPDNSLocalSites();
    }
  } catch (err) {
    console.error('Failed to load sites:', err);
  }
}

// Backward compatibility alias
function loadPDNSSites() {
  if (typeof loadPDNSLocalSites === 'function') {
    return loadPDNSLocalSites();
  }
}

function openCreateSiteModal() {
  document.getElementById('modal-create-site').classList.add('active');
}

function closeCreateSiteModal() {
  document.getElementById('modal-create-site').classList.remove('active');
}

async function submitCreateSite() {
  const domain = document.getElementById('site-domain').value.trim();
  const framework = document.getElementById('site-framework').value;
  const phpVer = document.getElementById('site-php-ver').value;
  const rootPath = document.getElementById('site-root-path').value.trim();
  const proxyPort = parseInt(document.getElementById('site-proxy-port').value, 10) || 0;
  const ssl = document.getElementById('site-ssl').checked;

  if (!domain) {
    await modalAlert('Domain wajib diisi (contoh: myapp.test)', 'warning', 'Validasi Input');
    return;
  }

  const payload = {
    domain: domain,
    framework: framework,
    php_version: phpVer,
    root_path: rootPath,
    proxy_port: proxyPort,
    ssl_enabled: ssl
  };

  try {
    const res = await fetch('/api/sites', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal membuat virtual host: ${data.error}`, 'error', 'Virtual Host');
      return;
    }
    closeCreateSiteModal();
    loadSites();
    await modalAlert(`Virtual host ${domain} berhasil dibuat!`, 'success', 'Virtual Host');
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Virtual Host');
  }
}

async function deleteSite(domain) {
  const confirmed = await modalConfirm(`Hapus virtual host ${domain}? Tindakan ini menghapus konfigurasi Nginx dan entri DNS lokal.`, {
    title: 'Hapus Virtual Host',
    confirmText: 'Hapus',
    type: 'danger'
  });
  if (!confirmed) return;

  try {
    const res = await fetch(`/api/sites/${domain}`, { method: 'DELETE' });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal: ${data.error}`, 'error', 'Hapus Virtual Host');
      return;
    }
    loadSites();
    await modalAlert(`Virtual host ${domain} berhasil dihapus.`, 'info', 'Hapus Virtual Host');
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Hapus Virtual Host');
  }
}

// Switchers
async function loadSwitchers() {
  try {
    const res = await fetch('/api/switchers');
    if (!res.ok) return;
    const data = await res.json();
    const container = document.getElementById('switchers-container');
    if (!container) return;
    container.innerHTML = '';

    for (const [key, item] of Object.entries(data)) {
      const card = document.createElement('div');
      card.className = 'service-card';
      let buttonsHtml = '';
      (item.available_versions || []).forEach(v => {
        let isCurrent = false;
        if (item.active_major) {
          isCurrent = (item.active_major === v || item.active_major === v.replace(/^v/, ''));
        } else {
          const cleanActive = (item.active_version || '').replace(/^v/, '').trim();
          const cleanV = v.replace(/^v/, '').trim();
          isCurrent = (cleanActive === cleanV || cleanActive.startsWith(cleanV + '.'));
        }
        buttonsHtml += `
          <button type="button" class="btn btn-sm ${isCurrent ? 'btn-primary' : 'btn-outline'}" onclick="switchRuntime('${key}', '${v}')" ${isCurrent ? 'title="Versi aktif saat ini"' : `title="Klik untuk beralih ke versi ${v}"`}>
            ${isCurrent ? '✓ ' + v : v}
          </button>
        `;
      });

      const switcherCfgBtn = (key === 'php' || key === 'pg') && item.active_version && item.active_version !== 'None'
        ? `<button type="button" class="btn btn-sm btn-outline" onclick="openServiceConfigModal('${key === 'php' ? 'php-fpm' : 'postgresql'}', '${escapeHtml(item.active_major || '')}')" title="Konfigurasi ${escapeHtml(item.name)}" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg> Config</button>`
        : '';

      card.innerHTML = `
        <div style="display:flex;justify-content:space-between;align-items:flex-start;gap:8px;">
          <div>
            <div class="service-name">${item.name}</div>
            <div class="service-meta">Versi Aktif: <strong>${item.active_version}</strong></div>
          </div>
          ${switcherCfgBtn}
        </div>
        <div style="margin-top:10px;">
          <label style="font-size:11px;color:var(--text-dim);font-weight:600;display:block;margin-bottom:6px;">GANTI KE VERSI:</label>
          <div style="display:flex;gap:6px;flex-wrap:wrap;">
            ${buttonsHtml}
          </div>
        </div>
      `;
      container.appendChild(card);
    }
  } catch (err) {
    console.error('Failed to load switchers:', err);
  }
}

async function executePrivilegedAction({
  confirmTitle,
  confirmMessage,
  confirmText = 'Lanjutkan',
  sudoMessage,
  url,
  payload,
  errorTitle,
  errorPrefix = 'Gagal',
  taskTitle
}) {
  if (isPrivilegedActionPending) return;
  isPrivilegedActionPending = true;

  try {
    if (document.activeElement && typeof document.activeElement.blur === 'function') {
      document.activeElement.blur();
    }

    const confirmed = await modalConfirm(confirmMessage, {
      title: confirmTitle,
      confirmText,
      type: 'primary'
    });
    if (!confirmed) return;

    const bodyPayload = { ...payload };
    let promptMsg = sudoMessage;

    while (true) {
      const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(bodyPayload)
      });
      const data = await res.json().catch(() => ({}));

      if (data.status === 'sudo_required' || res.status === 401) {
        if (res.status === 401) {
          promptMsg = 'Password sudo tidak valid. Silakan masukkan kembali password sudo Anda:';
        }
        const pass = await modalPrompt(promptMsg, {
          title: 'Akses Sudo Diperlukan',
          placeholder: 'Masukkan password sudo...',
          confirmText: 'Lanjutkan',
          cancelText: 'Batal',
          isPassword: true
        });
        if (!pass) return;
        bodyPayload.sudo_pass = pass;
        continue;
      }

      if (!res.ok) {
        await modalAlert(`${errorPrefix}: ${data.error || 'Terjadi kesalahan'}`, 'error', errorTitle);
        return;
      }

      openTerminalModal(taskTitle);
      return;
    }
  } catch (err) {
    await modalAlert('Error koneksi: ' + err, 'error', errorTitle);
  } finally {
    isPrivilegedActionPending = false;
  }
}

async function switchRuntime(type, ver) {
  const labelMap = {
    'node': 'Node.js',
    'php': 'PHP',
    'pg': 'PostgreSQL',
    'java': 'Java OpenJDK'
  };
  const label = labelMap[type] || type.toUpperCase();

  await executePrivilegedAction({
    confirmTitle: 'Konfirmasi Pengalihan Runtime',
    confirmMessage: `Beralih versi runtime ${label} ke versi ${ver}?`,
    confirmText: 'Beralih Sekarang',
    sudoMessage: `Pengalihan runtime ${label} memerlukan hak akses administrator (sudo) sistem.\nSilakan masukkan password sudo Anda:`,
    url: `/api/switchers/${type}`,
    payload: { version: ver },
    errorTitle: 'Ganti Runtime',
    errorPrefix: 'Gagal beralih runtime',
    taskTitle: `Switch ${label} -> ${ver}`
  });
}

// Cloudflare
async function loadCloudflare() {
  try {
    const res = await fetch('/api/cloudflare/status');
    if (!res.ok) return;
    const data = await res.json();

    document.getElementById('cf-status-badge').innerHTML = data.active ?
      '<span class="status-pill active">● Tunnel Berjalan</span>' :
      '<span class="status-pill stopped">○ Tunnel Nonaktif</span>';

    const profilesDiv = document.getElementById('cf-profiles-list');
    if (profilesDiv) {
      profilesDiv.innerHTML = (data.profiles && data.profiles.length > 0) ?
        data.profiles.map(p => `<span class="stat-badge">${p}</span>`).join(' ') :
        '<span style="color:var(--text-dim);">Belum ada profil terdaftar.</span>';
    }

    const exposuresTbody = document.getElementById('cf-exposures-tbody');
    if (exposuresTbody) {
      exposuresTbody.innerHTML = '';
      if (!data.exposures || data.exposures.length === 0) {
        exposuresTbody.innerHTML = `<tr><td colspan="4" style="text-align:center;color:var(--text-dim);padding:16px;">Belum ada exposure Cloudflare Tunnel.</td></tr>`;
      } else {
        data.exposures.forEach(exp => {
          const tr = document.createElement('tr');
          tr.innerHTML = `
            <td><code>${exp.local_domain}</code></td>
            <td><strong><a href="https://${exp.public_host}" target="_blank" style="color:var(--primary);">${exp.public_host}</a></strong></td>
            <td>${exp.profile}</td>
            <td><button class="btn btn-sm btn-danger" onclick="unexposeCF('${exp.local_domain}')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg> Hapus</button></td>
          `;
          exposuresTbody.appendChild(tr);
        });
      }
    }
  } catch (err) {
    console.error('Failed to load Cloudflare:', err);
  }
}

async function submitCFExpose() {
  const local = document.getElementById('cf-local-domain').value.trim();
  const publicHost = document.getElementById('cf-public-host').value.trim();
  if (!local || !publicHost) {
    await modalAlert('Domain lokal dan domain publik wajib diisi.', 'warning', 'Cloudflare Tunnel');
    return;
  }
  try {
    const res = await fetch('/api/cloudflare/expose', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ local_domain: local, public_host: publicHost })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal: ${data.error}`, 'error', 'Cloudflare Tunnel');
    } else {
      await modalAlert(`Eksposur berhasil dibuat untuk ${local} -> ${publicHost}`, 'success', 'Cloudflare Tunnel');
    }
    loadCloudflare();
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Cloudflare Tunnel');
  }
}

async function unexposeCF(localDomain) {
  const confirmed = await modalConfirm(`Hapus exposure Cloudflare untuk ${localDomain}?`, {
    title: 'Hapus Eksposur',
    confirmText: 'Hapus',
    type: 'danger'
  });
  if (!confirmed) return;

  try {
    await fetch('/api/cloudflare/unexpose', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ local_domain: localDomain })
    });
    loadCloudflare();
    await modalAlert(`Eksposur Cloudflare untuk ${localDomain} berhasil dihapus.`, 'info', 'Cloudflare Tunnel');
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Cloudflare Tunnel');
  }
}

// ==========================================
// PowerDNS & VPS Edge Gateway
// ==========================================
let currentPDNSZone = '';
let currentPDNSExposeMode = 'site';
let currentVPSAuthMethod = 'password';
let cachedSites = [];
let cachedProxies = [];
let cachedZones = [];
let cachedLocalNetbirdIP = '-';

function setVPSAuthMethod(method) {
  currentVPSAuthMethod = method;
  const btnPass = document.getElementById('vps-auth-btn-pass');
  const btnKey = document.getElementById('vps-auth-btn-key');
  const grpPass = document.getElementById('vps-auth-pass-group');
  const grpKey = document.getElementById('vps-auth-key-group');

  if (method === 'password') {
    if (btnPass) btnPass.className = 'btn btn-primary btn-sm';
    if (btnKey) btnKey.className = 'btn btn-outline btn-sm';
    if (grpPass) grpPass.style.display = 'block';
    if (grpKey) grpKey.style.display = 'none';
  } else {
    if (btnPass) btnPass.className = 'btn btn-outline btn-sm';
    if (btnKey) btnKey.className = 'btn btn-primary btn-sm';
    if (grpPass) grpPass.style.display = 'none';
    if (grpKey) grpKey.style.display = 'block';
  }
}

function openVPSDeployModal() {
  const modal = document.getElementById('modal-vps-deploy');
  if (modal) modal.classList.add('active');
}

function closeVPSDeployModal() {
  const modal = document.getElementById('modal-vps-deploy');
  if (modal) modal.classList.remove('active');
}

async function submitVPSDeploy() {
  const host = document.getElementById('vps-deploy-host')?.value.trim();
  const user = document.getElementById('vps-deploy-user')?.value.trim() || 'root';
  const port = parseInt(document.getElementById('vps-deploy-port')?.value, 10) || 22;
  const pass = document.getElementById('vps-deploy-password')?.value || '';
  const keyPath = document.getElementById('vps-deploy-keypath')?.value.trim() || '';

  if (!host) {
    await modalAlert('Host / IP VPS wajib diisi (contoh IP NetBird atau IP Publik).', 'warning', 'Validasi Input');
    return;
  }
  if (currentVPSAuthMethod === 'password' && !pass) {
    await modalAlert('Password SSH wajib diisi untuk login ke VPS.', 'warning', 'Validasi Input');
    return;
  }

  const confirmed = await modalConfirm(`Mulai deployment otomatis ke VPS ${user}@${host}:${port}? Skrip instalasi PowerDNS, Nginx, dan Edge Gateway akan dijalankan di VPS via SSH.`, {
    title: 'Konfirmasi Deploy VPS',
    confirmText: 'Mulai Deploy'
  });
  if (!confirmed) return;

  try {
    const res = await fetch('/api/pdns/deploy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        host: host,
        port: port,
        user: user,
        auth_method: currentVPSAuthMethod,
        password: pass,
        key_path: keyPath
      })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal memulai deploy: ${data.error || 'Terjadi kesalahan'}`, 'error', 'Deploy VPS');
      return;
    }
    closeVPSDeployModal();
    openTerminalModal('Deploy VPS Edge Gateway');
  } catch (err) {
    await modalAlert('Kesalahan koneksi: ' + err, 'error', 'Deploy VPS');
  }
}

function openVPSConnectModal() {
  const modal = document.getElementById('modal-vps-connect');
  if (modal) modal.classList.add('active');
}

function closeVPSConnectModal() {
  const modal = document.getElementById('modal-vps-connect');
  if (modal) modal.classList.remove('active');
}

async function submitVPSConnect() {
  const nbIP = document.getElementById('vps-conn-netbird-ip')?.value.trim();
  const pubIP = document.getElementById('vps-conn-public-ip')?.value.trim();
  const key = document.getElementById('vps-conn-api-key')?.value.trim();
  const pPort = document.getElementById('vps-conn-pdns-port')?.value.trim() || '8081';
  const aPort = document.getElementById('vps-conn-agent-port')?.value.trim() || '8082';

  if (!nbIP || !key) {
    await modalAlert('IP NetBird VPS dan PowerDNS API Key wajib diisi.', 'warning', 'Validasi Input');
    return;
  }

  try {
    const res = await fetch('/api/pdns/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        vps_netbird_ip: nbIP,
        vps_public_ip: pubIP,
        api_key: key,
        pdns_port: pPort,
        agent_port: aPort
      })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal menghubungkan ke VPS: ${data.error || 'Terjadi kesalahan'}`, 'error', 'Koneksi VPS');
      return;
    }
    closeVPSConnectModal();
    await modalAlert('Berhasil terhubung ke VPS Edge Gateway! Konfigurasi tersimpan.', 'success', 'Koneksi VPS');
    loadPDNS();
  } catch (err) {
    await modalAlert('Kesalahan koneksi: ' + err, 'error', 'Koneksi VPS');
  }
}

function setPDNSExposeMode(mode) {
  currentPDNSExposeMode = mode;
  const btnSite = document.getElementById('pdns-btn-mode-site');
  const btnPort = document.getElementById('pdns-btn-mode-port');
  const grpSite = document.getElementById('pdns-expose-site-group');
  const grpPort = document.getElementById('pdns-expose-port-group');

  if (mode === 'site') {
    if (btnSite) {
      btnSite.className = 'btn btn-primary btn-sm';
      btnSite.setAttribute('aria-selected', 'true');
    }
    if (btnPort) {
      btnPort.className = 'btn btn-outline btn-sm';
      btnPort.setAttribute('aria-selected', 'false');
    }
    if (grpSite) grpSite.style.display = 'block';
    if (grpPort) grpPort.style.display = 'none';
  } else {
    if (btnSite) {
      btnSite.className = 'btn btn-outline btn-sm';
      btnSite.setAttribute('aria-selected', 'false');
    }
    if (btnPort) {
      btnPort.className = 'btn btn-primary btn-sm';
      btnPort.setAttribute('aria-selected', 'true');
    }
    if (grpSite) grpSite.style.display = 'none';
    if (grpPort) grpPort.style.display = 'block';
  }
}

async function loadPDNS() {
  try {
    const res = await fetch('/api/pdns/status');
    if (!res.ok) return;
    const conf = await res.json();
    cachedLocalNetbirdIP = conf.local_netbird_ip || '-';

    const badge = document.getElementById('pdns-status-badge');
    if (badge) {
      badge.innerHTML = conf.reachable ?
        '<span class="status-pill active" title="Edge VPS terhubung"><span class="pulse-dot pulse-success"></span><span>Terhubung ke Edge VPS</span></span>' :
        '<span class="status-pill stopped" title="Edge VPS terputus"><span class="pulse-dot pulse-danger"></span><span>Edge VPS Terputus</span></span>';
    }

    const deployBtn = document.getElementById('btn-pdns-deploy');
    if (deployBtn) {
      deployBtn.className = conf.reachable ? 'btn btn-outline btn-sm' : 'btn btn-primary btn-sm';
    }

    const nbEl = document.getElementById('pdns-netbird-ip');
    if (nbEl) nbEl.textContent = conf.vps_netbird_ip || '-';

    const pubEl = document.getElementById('pdns-public-ip');
    if (pubEl) pubEl.textContent = conf.vps_public_ip || '-';

    const locEl = document.getElementById('pdns-local-ip');
    if (locEl) locEl.textContent = conf.local_netbird_ip || '-';

    // Banner status jika belum terhubung
    const banner = document.getElementById('pdns-unconnected-banner');
    if (banner) {
      banner.style.display = conf.reachable ? 'none' : 'block';
    }

    // Isi nilai awal form deploy & connect jika ada konfigurasi
    const depHost = document.getElementById('vps-deploy-host');
    if (depHost && !depHost.value && conf.vps_netbird_ip) {
      depHost.value = conf.vps_netbird_ip;
    }
    const connNb = document.getElementById('vps-conn-netbird-ip');
    if (connNb && !connNb.value && conf.vps_netbird_ip) {
      connNb.value = conf.vps_netbird_ip;
    }
    const connPub = document.getElementById('vps-conn-public-ip');
    if (connPub && !connPub.value && conf.vps_public_ip) {
      connPub.value = conf.vps_public_ip;
    }
    const connKey = document.getElementById('vps-conn-api-key');
    if (connKey && !connKey.value && conf.api_key) {
      connKey.value = conf.api_key;
    }
    const connPPort = document.getElementById('vps-conn-pdns-port');
    if (connPPort && conf.pdns_port) {
      connPPort.value = conf.pdns_port;
    }
    const connAPort = document.getElementById('vps-conn-agent-port');
    if (connAPort && conf.agent_port) {
      connAPort.value = conf.agent_port;
    }

    await loadPDNSExposures();
    await loadPDNSLocalSites();
    await loadPDNSZones();
  } catch (err) {
    console.error('Failed to load PDNS:', err);
  }
}

async function loadPDNSLocalSites() {
  const tbody = document.getElementById('pdns-local-sites-tbody');
  if (!tbody) return;

  try {
    const res = await fetch('/api/sites');
    if (!res.ok) return;
    cachedSites = await res.json() || [];

    tbody.innerHTML = '';
    if (cachedSites.length === 0) {
      tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:var(--text-dim);padding:20px;">Belum ada virtual host lokal. Buat virtual host di menu 'Web Stack' terlebih dahulu.</td></tr>`;
      return;
    }

    cachedSites.forEach(s => {
      const tr = document.createElement('tr');
      const isExposed = !!s.exposed_to;
      const targetNetbird = cachedLocalNetbirdIP !== '-' ? `http://${cachedLocalNetbirdIP}:80` : 'Port 80 (wt0)';

      tr.innerHTML = `
        <td>
          <div style="font-weight:700;font-size:13.5px;color:var(--text-main);">${s.domain}</div>
          <div style="font-size:11px;color:var(--text-muted);margin-top:2px;max-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${s.root_path || '-'}">${s.root_path || '-'}</div>
        </td>
        <td>
          <span class="stat-badge" style="font-size:11px;padding:2px 8px;">${(s.framework || 'PHP').toUpperCase()}</span>
          <span style="font-size:11px;color:var(--text-muted);margin-left:4px;">PHP ${s.php_version || '8.4'}</span>
        </td>
        <td>
          ${isExposed ?
            '<span class="status-pill active" style="font-weight:600;font-size:12px;">● TERHUBUNG KE INTERNET</span>' :
            '<span class="status-pill stopped" style="font-size:12px;">○ Hanya Lokal (Offline)</span>'}
        </td>
        <td>
          ${isExposed ?
            `<a href="https://${s.exposed_to}" target="_blank" rel="noopener" style="color:var(--primary);font-weight:700;font-size:13px;text-decoration:none;">https://${s.exposed_to} ↗</a>` :
            '<span style="color:var(--text-dim);font-style:italic;font-size:12px;">Belum diekspos</span>'}
        </td>
        <td><code>${targetNetbird}</code></td>
        <td>
          <div style="display:flex;gap:6px;align-items:center;">
            ${isExposed ?
              `<a href="https://${s.exposed_to}" target="_blank" rel="noopener" class="btn btn-sm btn-outline" style="text-decoration:none;display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg> Buka URL</a>
               <button class="btn btn-sm btn-danger" onclick="unexposePDNS('${s.exposed_to}')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg> Putus Ekspos</button>` :
              `<button class="btn btn-sm btn-primary" onclick="quickExposeSite('${s.domain}')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/><path d="M12 13v6"/><path d="m9 16 3-3 3 3"/></svg> Ekspos ke VPS</button>`}
          </div>
        </td>
      `;
      tbody.appendChild(tr);
    });

    loadPDNSSiteDropdown();
  } catch (err) {
    console.error('Failed to load local sites for PDNS:', err);
  }
}

function loadPDNSSiteDropdown(preselectedDomain = '') {
  const select = document.getElementById('pdns-expose-site-select');
  if (!select) return;

  const current = preselectedDomain || select.value;
  select.innerHTML = cachedSites.length === 0
    ? '<option value="">(Belum ada vhost lokal dibuat)</option>'
    : cachedSites.map(s => {
        const label = s.exposed_to
          ? `${s.domain} [✓ SUDAH DIEKSPOS -> ${s.exposed_to}]`
          : `${s.domain} [○ Belum Diekspos]`;
        return `<option value="${s.domain}">${label}</option>`;
      }).join('');

  if (current && cachedSites.some(s => s.domain === current)) {
    select.value = current;
  }
  onPDNSSiteSelected();
}

function onPDNSSiteSelected() {
  const select = document.getElementById('pdns-expose-site-select');
  const pubInput = document.getElementById('pdns-expose-site-pub');
  const alertBox = document.getElementById('pdns-expose-site-alert');
  if (!select || !pubInput) return;

  const selSite = cachedSites.find(s => s.domain === select.value);
  const defaultZone = (cachedZones.length > 0) ? cachedZones[0].name.replace(/\.$/, '') : 'erposoft.my.id';

  if (selSite) {
    if (selSite.exposed_to) {
      pubInput.value = selSite.exposed_to;
      if (alertBox) {
        alertBox.style.display = 'block';
        alertBox.style.backgroundColor = 'rgba(245, 158, 11, 0.12)';
        alertBox.style.color = 'var(--warning)';
        alertBox.style.border = '1px solid rgba(245, 158, 11, 0.3)';
        alertBox.innerHTML = `⚠️ Virtual host <strong>${selSite.domain}</strong> saat ini <strong>SUDAH DIEKSPOS</strong> ke <strong>https://${selSite.exposed_to}</strong>. Mengekspos ulang akan memperbarui konfigurasi Nginx dan sertifikat SSL Let's Encrypt.`;
      }
    } else {
      const cleanPrefix = selSite.domain.replace(/\.test$/, '').replace(/\.local$/, '');
      pubInput.value = cleanPrefix + '.' + defaultZone;
      if (alertBox) {
        alertBox.style.display = 'block';
        alertBox.style.backgroundColor = 'rgba(59, 130, 246, 0.12)';
        alertBox.style.color = 'var(--primary)';
        alertBox.style.border = '1px solid rgba(59, 130, 246, 0.3)';
        alertBox.innerHTML = `💡 Virtual host <strong>${selSite.domain}</strong> belum diekspos ke internet. Rekomendasi domain publik: <code>${pubInput.value}</code>`;
      }
    }
  } else {
    if (alertBox) alertBox.style.display = 'none';
  }
}

function openPDNSExposeModal(mode = 'site', targetDomain = '') {
  setPDNSExposeMode(mode);
  loadPDNSSiteDropdown(targetDomain);

  if (mode === 'port') {
    const portPub = document.getElementById('pdns-expose-port-pub');
    if (portPub && !portPub.value) {
      const defaultZone = (cachedZones.length > 0) ? cachedZones[0].name.replace(/\.$/, '') : 'erposoft.my.id';
      portPub.placeholder = `contoh: api.${defaultZone}`;
    }
  }

  const modal = document.getElementById('modal-pdns-expose');
  if (modal) modal.classList.add('active');
}

function closePDNSExposeModal() {
  const modal = document.getElementById('modal-pdns-expose');
  if (modal) modal.classList.remove('active');
}

function quickExposeSite(domain) {
  openPDNSExposeModal('site', domain);
}

async function loadPDNSExposures() {
  const tbody = document.getElementById('pdns-exposures-tbody');
  if (!tbody) return;

  try {
    const res = await fetch('/api/pdns/exposures');
    if (!res.ok) {
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:16px;">Gagal memuat proxy VPS.</td></tr>`;
      return;
    }
    cachedProxies = await res.json() || [];
    tbody.innerHTML = '';
    if (cachedProxies.length === 0) {
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:16px;">Belum ada proxy atau aplikasi yang diekspos di VPS Edge.</td></tr>`;
      return;
    }

    cachedProxies.forEach(p => {
      const tr = document.createElement('tr');
      const isVhost = !!p.local_site;
      const targetBadge = isVhost
        ? `<span class="stat-badge" style="background:rgba(59,130,246,0.15);color:var(--primary);margin-right:6px;">VHost</span> <strong>${p.local_site}</strong>`
        : `<span class="stat-badge" style="background:rgba(100,116,139,0.15);color:var(--text-muted);margin-right:6px;">Service</span> ${p.target || 'Proxy VPS'}`;

      tr.innerHTML = `
        <td>
          <a href="https://${p.domain}" target="_blank" rel="noopener" style="color:var(--primary);font-weight:700;font-size:13.5px;text-decoration:none;">
            ${p.domain} ↗
          </a>
        </td>
        <td>${targetBadge}</td>
        <td>
          <span class="status-pill ${p.enabled ? 'active' : 'stopped'}">
            ${p.enabled ? '● Aktif (HTTPS Let\'s Encrypt)' : '○ Nonaktif'}
          </span>
        </td>
        <td><code style="font-size:11.5px;color:var(--text-muted);max-width:180px;display:inline-block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:bottom;" title="${p.config_file || '-'}">${p.config_file || '-'}</code></td>
        <td>
          <button class="btn btn-sm btn-danger" onclick="unexposePDNS('${p.domain}')" style="display:inline-flex;align-items:center;gap:4px;">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
            <span>Hapus Ekspos</span>
          </button>
        </td>
      `;
      tbody.appendChild(tr);
    });
  } catch (err) {
    console.error('Failed to load PDNS exposures:', err);
    tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:16px;">Edge VPS tidak dapat dihubungi.</td></tr>`;
  }
}

async function submitPDNSExpose() {
  const useSSL = document.getElementById('pdns-expose-ssl')?.checked ?? true;
  const ws = document.getElementById('pdns-expose-ws')?.checked ?? true;

  let payload = {
    mode: currentPDNSExposeMode,
    use_ssl: useSSL,
    websocket: ws
  };

  if (currentPDNSExposeMode === 'site') {
    const localDom = document.getElementById('pdns-expose-site-select')?.value.trim();
    const pubDom = document.getElementById('pdns-expose-site-pub')?.value.trim();
    if (!localDom) {
      await modalAlert('Pilih vhost lokal yang ingin diekspos.', 'warning', 'Validasi Input');
      return;
    }
    if (!pubDom) {
      await modalAlert('Domain publik VPS wajib diisi (contoh: portal.erposoft.my.id).', 'warning', 'Validasi Input');
      return;
    }
    payload.local_domain = localDom;
    payload.public_domain = pubDom;
  } else {
    const pubDom = document.getElementById('pdns-expose-port-pub')?.value.trim();
    const portTarget = parseInt(document.getElementById('pdns-expose-port-target')?.value, 10);
    if (!pubDom) {
      await modalAlert('Domain publik VPS wajib diisi (contoh: api.erposoft.my.id).', 'warning', 'Validasi Input');
      return;
    }
    if (!portTarget || portTarget <= 0 || portTarget > 65535) {
      await modalAlert('Port target lokal tidak valid (1-65535).', 'warning', 'Validasi Input');
      return;
    }
    payload.public_domain = pubDom;
    payload.target_port = portTarget;
  }

  const confirmed = await modalConfirm(`Ekspos '${payload.public_domain}' ke internet via VPS Edge Gateway?`, {
    title: 'Konfirmasi Ekspos VPS',
    confirmText: 'Ekspos Sekarang',
    type: 'info'
  });
  if (!confirmed) return;

  try {
    const res = await fetch('/api/pdns/expose', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal ekspos ke VPS: ${data.error || 'Terjadi kesalahan'}`, 'error', 'VPS Edge Gateway');
    } else {
      closePDNSExposeModal();
      await modalAlert(`Domain '${payload.public_domain}' berhasil diekspos ke internet! DNS dan reverse-proxy di VPS telah aktif.`, 'success', 'VPS Edge Gateway');
      loadPDNS();
      loadSites();
    }
  } catch (err) {
    await modalAlert('Kesalahan koneksi: ' + err, 'error', 'VPS Edge Gateway');
  }
}

async function unexposePDNS(domain) {
  const confirmed = await modalConfirm(`Hapus ekspos domain '${domain}' dari VPS Edge Gateway? Konfigurasi proxy dan record DNS subdomain akan dibersihkan.`, {
    title: 'Hapus Eksposur VPS',
    confirmText: 'Hapus Ekspos',
    danger: true
  });
  if (!confirmed) return;

  try {
    const res = await fetch('/api/pdns/unexpose', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain: domain, keep_dns: false })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal menghapus ekspos: ${data.error}`, 'error', 'VPS Edge Gateway');
    } else {
      await modalAlert(`Ekspos untuk '${domain}' berhasil dihapus dari VPS Edge.`, 'info', 'VPS Edge Gateway');
      loadPDNS();
      loadSites();
    }
  } catch (err) {
    await modalAlert('Kesalahan jaringan: ' + err, 'error', 'VPS Edge Gateway');
  }
}

async function loadPDNSZones() {
  const tbody = document.getElementById('pdns-zones-tbody');
  if (!tbody) return;

  try {
    const res = await fetch('/api/pdns/zones');
    if (!res.ok) {
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:16px;">Gagal memuat zone DNS.</td></tr>`;
      return;
    }
    cachedZones = await res.json() || [];
    tbody.innerHTML = '';
    if (cachedZones.length === 0) {
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:16px;">Belum ada zone DNS.</td></tr>`;
      return;
    }

    cachedZones.forEach(z => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td><strong>${z.name}</strong></td>
        <td>${z.kind}</td>
        <td>${z.serial}</td>
        <td><span class="status-pill active">● Aktif</span></td>
        <td>
          <div style="display:flex;gap:6px;">
            <button class="btn btn-sm btn-outline" onclick="openPDNSRecordsModal('${z.name}')" style="display:inline-flex;align-items:center;gap:4px;">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>
              <span>Kelola Record</span>
            </button>
 <button class="btn btn-sm btn-outline" onclick="doctorPDNSZone('${z.name}')" style="display:inline-flex;align-items:center;gap:4px;">
 <span>Doctor</span>
 </button>
 <button class="btn btn-sm btn-outline" onclick="repairPDNSZone('${z.name}')" style="display:inline-flex;align-items:center;gap:4px;">
 <span>Repair</span>
 </button>
            <button class="btn btn-sm btn-danger" onclick="deletePDNSZone('${z.name}')" style="display:inline-flex;align-items:center;gap:4px;">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
              <span>Hapus</span>
            </button>
          </div>
        </td>
      `;
      tbody.appendChild(tr);
    });
  } catch (err) {
    console.error('Failed to load zones:', err);
  }
}

async function repairPDNSZone(zoneName) {
 const confirmed = await modalConfirm(`Perbaiki zone ${zoneName}? Tindakan ini menulis ulang SOA/NS, glue ns1/ns2, dan mengaktifkan SOA-EDIT-API. Record lain tidak diubah.`, {
 title: 'Repair DNS Zone', confirmText: 'Repair Zone'
 });
 if (!confirmed) return;
 try {
 const res = await fetch('/api/pdns/zone/repair', {
 method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ zone: zoneName })
 });
 const r = await res.json().catch(() => ({}));
 const icon = (s) => s === 'ok' ? '✓' : s === 'warn' ? '!' : '✗';
 const rep = r.report || r;
 const lines = [];
 if (r.error) lines.push('Error: ' + r.error, '');
 lines.push(`Zone: ${rep.zone || zoneName}`, '');
 (rep.checks || []).forEach(c => lines.push(`${icon(c.status)} ${c.name}: ${c.detail}`));
 await modalAlert(lines.join('\n'), r.error ? 'error' : (rep.ok ? 'success' : 'warning'), 'Repair Zone');
 loadPDNSZones();
 } catch (err) {
 await modalAlert('Error: ' + err, 'error', 'Repair Zone');
 }
}

async function doctorPDNSZone(zoneName) {
 try {
 const res = await fetch('/api/pdns/zone/doctor?zone=' + encodeURIComponent(zoneName));
 const r = await res.json().catch(() => ({}));
 if (!res.ok && !r.checks) {
 await modalAlert(`Gagal menjalankan doctor: ${r.error || res.statusText}`, 'error', 'Doctor Zone');
 return;
 }
 const icon = (s) => s === 'ok' ? '✓' : s === 'warn' ? '!' : '✗';
 const lines = [`Zone: ${r.zone || zoneName}`, `Nameserver: ${(r.nameservers || []).join(', ') || '-'}`, ''];
 (r.checks || []).forEach(c => lines.push(`${icon(c.status)} ${c.name}: ${c.detail}`));
 await modalAlert(lines.join('\n'), r.ok ? 'success' : 'warning', 'Doctor Zone');
 } catch (err) {
 await modalAlert('Error: ' + err, 'error', 'Doctor Zone');
 }
}

async function submitAddZone() {
  const zone = await modalPrompt('Masukkan nama zone domain authoritative (contoh: domainanda.com):', {
    title: 'Tambah DNS Zone',
    placeholder: 'domainanda.com'
  });
  if (!zone) return;
 const clean = String(zone).trim().replace(/\.$/, '').toLowerCase();
 if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(clean)) {
 await modalAlert('Nama zone tidak valid. Gunakan FQDN tanpa protokol, contoh: domainanda.com', 'warning', 'Validasi Zone');
 return;
 }

  try {
    const res = await fetch('/api/pdns/zones', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ zone_name: clean })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal: ${data.error}`, 'error', 'PowerDNS');
    } else {
      await modalAlert(`Zone DNS ${zone} berhasil ditambahkan!`, 'success', 'PowerDNS');
      loadPDNSZones();
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'PowerDNS');
  }
}

async function deletePDNSZone(zoneName) {
  const confirmed = await modalConfirm(`Hapus authoritative zone '${zoneName}' beserta seluruh record di dalamnya dari PowerDNS VPS?`, {
    title: 'Hapus Authoritative Zone',
    confirmText: 'Hapus Zone',
    danger: true
  });
  if (!confirmed) return;

  try {
    const res = await fetch('/api/pdns/zones', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ zone_name: zoneName })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal: ${data.error}`, 'error', 'PowerDNS');
    } else {
      await modalAlert(`Zone DNS '${zoneName}' berhasil dihapus.`, 'info', 'PowerDNS');
      loadPDNSZones();
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'PowerDNS');
  }
}

// ==========================================
// DNS Records Modal Dialog
// ==========================================
function openPDNSRecordsModal(zoneName) {
  currentPDNSZone = zoneName;
  const modal = document.getElementById('modal-pdns-records');
  const title = document.getElementById('modal-pdns-records-title');
  if (title) title.textContent = `Record DNS — ${zoneName}`;
  if (modal) modal.classList.add('active');
  loadPDNSRecords(zoneName);
}

function closePDNSRecordsModal() {
  const modal = document.getElementById('modal-pdns-records');
  if (modal) modal.classList.remove('active');
}

async function loadPDNSRecords(zoneName) {
  const tbody = document.getElementById('pdns-records-tbody');
  if (!tbody) return;
  tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:14px;">Memuat record DNS...</td></tr>`;

  try {
    const res = await fetch(`/api/pdns/records?zone=${encodeURIComponent(zoneName)}`);
    if (!res.ok) {
      const data = await res.json();
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:14px;">Gagal memuat record: ${data.error}</td></tr>`;
      return;
    }
    const rrsets = await res.json() || [];
    tbody.innerHTML = '';
    if (rrsets.length === 0) {
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:14px;">Belum ada record pada zone ini.</td></tr>`;
      return;
    }

    rrsets.forEach(rr => {
      const isProtected = (rr.type === 'SOA' || rr.type === 'NS');
      const cleanName = rr.name.replace(/\.$/, '');
      const isEdgeProxy = cachedProxies.some(p => p.domain === cleanName);

      (rr.records || []).forEach(rec => {
        const tr = document.createElement('tr');
        const proxyBadge = isEdgeProxy
          ? '<span class="stat-badge" style="background:rgba(59,130,246,0.18);color:var(--primary);font-size:10px;margin-left:6px;border:1px solid rgba(59,130,246,0.3);">● Edge VPS Proxy</span>'
          : '';

        tr.innerHTML = `
          <td><strong>${rr.name}</strong>${proxyBadge}</td>
          <td><span class="stat-badge" style="font-size:11px;padding:2px 6px;">${rr.type}</span></td>
          <td>${rr.ttl}</td>
          <td style="max-width:260px;word-break:break-all;font-size:12px;font-family:monospace;">${rec.content}</td>
          <td>
            ${isProtected ? '<span style="font-size:11px;color:var(--text-dim);">Sistem</span>' :
              `<button class="btn btn-sm btn-danger" style="padding:2px 8px;font-size:11px;display:inline-flex;align-items:center;gap:3px;" onclick="deletePDNSRecord('${zoneName}', '${rr.name}', '${rr.type}')"><svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg> Hapus</button>`}
          </td>
        `;
        tbody.appendChild(tr);
      });
    });
  } catch (err) {
    console.error('Failed to load records:', err);
    tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:14px;">Kesalahan jaringan: ${err}</td></tr>`;
  }
}

async function submitAddPDNSRecord() {
  if (!currentPDNSZone) return;

  const name = document.getElementById('pdns-rec-name')?.value.trim();
  const type = document.getElementById('pdns-rec-type')?.value.trim() || 'A';
  const content = document.getElementById('pdns-rec-content')?.value.trim();
  const ttl = parseInt(document.getElementById('pdns-rec-ttl')?.value, 10) || 300;

  if (!content) {
    await modalAlert('Konten / nilai target record tidak boleh kosong.', 'warning', 'Validasi Input');
    return;
  }

  try {
    const res = await fetch('/api/pdns/records', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        zone: currentPDNSZone,
        name: name || '@',
        type: type,
        content: content,
        ttl: ttl
      })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal menambahkan record: ${data.error}`, 'error', 'PowerDNS');
    } else {
      document.getElementById('pdns-rec-name').value = '';
      document.getElementById('pdns-rec-content').value = '';
      loadPDNSRecords(currentPDNSZone);
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'PowerDNS');
  }
}

async function deletePDNSRecord(zone, name, type) {
  const confirmed = await modalConfirm(`Hapus record DNS ${type} untuk '${name}'?`, {
    title: 'Hapus Record DNS',
    confirmText: 'Hapus',
    danger: true
  });
  if (!confirmed) return;

  try {
    const res = await fetch('/api/pdns/records', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ zone: zone, name: name, type: type })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal menghapus record: ${data.error}`, 'error', 'PowerDNS');
    } else {
      loadPDNSRecords(zone);
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'PowerDNS');
  }
}

// ==========================================
// TERMINAL MODAL CONTROLLER & SSE STREAM
// ==========================================
let eventSource = null;
let isTerminalTaskRunning = false;

function openTerminalModal(taskName = '') {
  const modal = document.getElementById('modal-terminal');
  if (!modal) return;
  modal.classList.add('active');

  if (taskName) {
    const taskLabel = document.getElementById('terminal-task-name');
    if (taskLabel) taskLabel.textContent = taskName;
  }

  const term = document.getElementById('terminal-log-output');
  if (term) {
    term.scrollTop = term.scrollHeight;
  }
}

function closeTerminalModal() {
  const modal = document.getElementById('modal-terminal');
  if (modal) modal.classList.remove('active');
}

function isTerminalModalOpen() {
  const modal = document.getElementById('modal-terminal');
  return modal ? modal.classList.contains('active') : false;
}

function clearTerminal() {
  const terminal = document.getElementById('terminal-log-output');
  if (terminal) {
    terminal.innerHTML = '<div style="color:var(--text-dim);font-style:italic;">[Layar konsol dibersihkan]</div>';
  }
}

async function copyTerminalLog() {
  const terminal = document.getElementById('terminal-log-output');
  const copyBtnText = document.getElementById('copy-terminal-text');
  if (!terminal) return;

  const text = terminal.innerText || terminal.textContent || '';
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    if (copyBtnText) {
      const orig = copyBtnText.textContent;
      copyBtnText.textContent = '✓ Tersalin!';
      setTimeout(() => { copyBtnText.textContent = orig; }, 1500);
    }
  } catch (err) {
    console.warn('Gagal menyalin log:', err);
  }
}

function setupSSE() {
  if (eventSource) eventSource.close();
  eventSource = new EventSource('/api/tasks/events');

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      if (data.type === 'ping') return;
      appendTerminalLog(data);
    } catch (e) {}
  };

  eventSource.onerror = () => {
    // Reconnect silently handled by browser EventSource
  };
}

function appendTerminalLog(data) {
  const terminal = document.getElementById('terminal-log-output');
  if (!terminal) return;

  // Clear initial placeholder if present
  if (terminal.textContent.includes('Menunggu perintah... Pilih salah satu menu')) {
    terminal.innerHTML = '';
  }

  const statusBadge = document.getElementById('terminal-status-badge');
  const taskLabel = document.getElementById('terminal-task-name');
  const pulseDot = document.getElementById('terminal-stream-pulse');
  const activeDotTopbar = document.getElementById('terminal-active-dot');
  const footerStatus = document.getElementById('terminal-footer-status');

  const line = document.createElement('div');
  line.style.marginBottom = '2px';

  if (data.type === 'start') {
    isTerminalTaskRunning = true;
    line.style.color = '#38bdf8';
    line.style.fontWeight = 'bold';
    line.textContent = `>>> [${new Date(data.timestamp).toLocaleTimeString()}] ${data.message}`;

    const cleanTask = (data.message || '').replace(/^Memulai:\s*/, '');
    if (taskLabel && cleanTask) taskLabel.textContent = cleanTask;

    if (statusBadge) {
      statusBadge.className = 'stat-badge terminal-badge-running';
      statusBadge.innerHTML = '<span class="pulse-dot"></span> Berjalan...';
    }
    if (pulseDot) pulseDot.style.display = 'inline-block';
    if (activeDotTopbar) activeDotTopbar.style.display = 'block';
    if (footerStatus) footerStatus.textContent = `Sedang mengeksekusi: ${cleanTask}...`;

    // Ensure terminal modal pops up automatically when task starts
    openTerminalModal(cleanTask);

  } else if (data.type === 'finish') {
    isTerminalTaskRunning = false;
    const isSuccess = (data.status === 'success');
    line.style.color = isSuccess ? '#10b981' : '#ef4444';
    line.style.fontWeight = 'bold';
    line.textContent = `<<< [${new Date(data.timestamp).toLocaleTimeString()}] ${data.message}`;

    if (statusBadge) {
      statusBadge.className = isSuccess ? 'stat-badge terminal-badge-success' : 'stat-badge terminal-badge-error';
      statusBadge.innerHTML = isSuccess ? '✓ Selesai' : '✕ Gagal';
    }
    if (pulseDot) pulseDot.style.display = 'none';
    if (activeDotTopbar) activeDotTopbar.style.display = 'none';
    if (footerStatus) {
      footerStatus.textContent = isSuccess
        ? '✓ Proses berhasil diselesaikan.'
        : `✕ Proses berakhir dengan kesalahan (${data.status}).`;
    }

    // Refresh status cards in background once task finishes
    setTimeout(loadStatus, 1000);
    setTimeout(loadSwitchers, 1100);
    setTimeout(loadToolchains, 1200);
    setTimeout(loadWorkspace, 1300);
 setTimeout(loadMail, 1400);

  } else {
    // Regular log message
    line.textContent = data.message;

    // If modal is not open and we're receiving active task output, open modal!
    if (!isTerminalModalOpen() && data.message && !data.message.startsWith(': heartbeat')) {
      openTerminalModal();
    }
  }

  terminal.appendChild(line);
  terminal.scrollTop = terminal.scrollHeight;
}

const COMPONENT_TITLES = {
  'rust': 'Rust Toolchain, Mold Linker & Sccache',
  'go': 'Golang (Go Language Toolchain)',
  'node': 'Node.js LTS & NPM',
  'java': 'OpenJDK Java JDK',
  'flutter': 'Flutter SDK Channel Stable',
  'android': 'Android SDK & CLI Tools',
  'redis': 'Redis Cache Server',
  'base': 'Dependensi Dasar Sistem',
  'update': 'Perbarui Seluruh Toolchain & Paket Sistem',
  'full-install': 'Instalasi Lengkap (Full Auto Setup)',
  'check-updates': 'Pemeriksaan Ketersediaan Pembaruan',
  'self-check': 'Validasi & Self-Check Sistem',
  'postgres': 'PostgreSQL Database Server',
  'nginx': 'Nginx Web Server',
  'php': 'PHP & PHP-FPM',
  'composer': 'Composer Package Manager',
  'ssl': 'Local SSL (mkcert)',
  'php-stack': 'Full PHP Web Stack',
  'cloudflare': 'Cloudflare Tunnel (cloudflared)',
  'pdns': 'PowerDNS & Netbird Edge Gateway',
  'zsh': 'Oh My Zsh Shell',
  'zsh-p10k': 'Oh My Zsh (Tema Powerlevel10k)',
  'zsh-agnoster': 'Oh My Zsh (Tema Agnoster)',
  'zsh-robbyrussell': 'Oh My Zsh (Tema Robbyrussell)',
  'orca': 'Orca ADE Desktop GUI',
  'orca-desktop': 'Orca ADE Desktop GUI',
  'orca-headless': 'Orca ADE Headless Server',
  'orca-update': 'Pembaruan Orca ADE',
  'orca-switch-desktop': 'Alih Mode Orca ADE ke Desktop GUI',
  'orca-switch-headless': 'Alih Mode Orca ADE ke Headless Server',
  'orca-uninstall': 'Uninstall Orca ADE',
  'orca-pair': 'Info Pairing Orca ADE',
  'orca-ai': 'Konfigurasi Orca Commit AI',
  'audit': 'Audit Duplikat Path & Toolchain',
  'audit-fix': 'Perbaikan Duplikat Path & Toolchain',
  'sync-hosts': 'Sinkronisasi Hosts & SSL Lokal',
  'cf-doctor': 'Diagnostik Cloudflare Tunnel'
};

async function triggerInstall(component) {
  const friendlyName = COMPONENT_TITLES[component] || component;
  await executePrivilegedAction({
    confirmTitle: 'Konfirmasi Instalasi / Pembaruan',
    confirmMessage: `Jalankan operasi instalasi / pembaruan untuk: ${friendlyName}?`,
    confirmText: 'Lanjutkan',
    sudoMessage: 'Operasi pemeliharaan/instalasi ini memerlukan hak akses administrator (sudo) sistem.\nSilakan masukkan password sudo Anda:',
    url: '/api/install',
    payload: { component },
    errorTitle: 'Instalasi',
    errorPrefix: 'Gagal',
    taskTitle: component
  });
}

// ==========================================
// MAIL SERVER & OUTBOUND RELAY
// ==========================================
let cachedMailStatus = null;
let cachedMailDomainAccounts = [];

async function loadMail() {
  loadMailStatus();
  loadMailAccounts();
  // Preload DNS zones agar ketersediaan domain selalu sinkron
  fetch('/api/pdns/zones')
    .then(r => r.ok ? r.json() : [])
    .then(z => { if (Array.isArray(z)) cachedZones = z; })
    .catch(() => {});
}

async function loadMailStatus() {
  const engineEl = document.getElementById('mail-engine-status');
  const webmailEl = document.getElementById('mail-webmail-status');
  const storageEl = document.getElementById('mail-storage-free');
  const tbody = document.getElementById('mail-domain-relay-tbody');
  const domainFilter = document.getElementById('mail-domain-filter');
  const relayDomainSelect = document.getElementById('mail-relay-domain');

  try {
    const res = await fetch('/api/mail/status');
    if (!res.ok) {
      const errData = await res.json().catch(() => ({}));
      if (tbody) tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;color:var(--text-dim);padding:14px;">Gagal memuat status mail: ${errData.error || res.statusText}</td></tr>`;
      return;
    }
    const data = await res.json();
    cachedMailStatus = data;

    // Metrics
    if (engineEl) {
      engineEl.innerHTML = data.stalwart_active
        ? '<span class="stat-badge stat-badge-ok">● Aktif (Running)</span>'
        : '<span class="stat-badge stat-badge-warn">● Nonaktif</span>';
    }
    if (webmailEl) {
      webmailEl.innerHTML = data.snappymail_active
        ? '<span class="stat-badge stat-badge-ok">● Siap / Aktif</span>'
        : '<span class="stat-badge stat-badge-warn">● Belum Terpasang</span>';
    }
    if (storageEl) {
      storageEl.textContent = data.storage_free ? `${data.storage_free} Bebas` : '-';
    }

    // Banner setup jika engine belum aktif atau domain kosong
    const banner = document.getElementById('mail-setup-banner');
    if (banner) {
      banner.style.display = (!data.stalwart_active || !data.domains || data.domains.length === 0) ? 'block' : 'none';
    }

    // Populate Domain Selects
    if (data.domains && data.domains.length > 0) {
      const currentFilter = domainFilter ? domainFilter.value : '';
      if (domainFilter) {
        domainFilter.innerHTML = '<option value="">Semua Domain</option>' +
          data.domains.map(d => `<option value="${d.domain}" ${d.domain === currentFilter ? 'selected' : ''}>${d.domain}</option>`).join('');
      }
      if (relayDomainSelect) {
 relayDomainSelect.innerHTML = '<option value="global">Semua Domain (relay default)</option>' + data.domains.map(d => `<option value="${d.domain}">${d.domain}</option>`).join('');
      }
 } else {
 if (domainFilter) {
 domainFilter.innerHTML = '<option value="">Semua Domain</option>';
 }
 if (relayDomainSelect) {
 relayDomainSelect.innerHTML = '<option value="global">Semua Domain (relay default)</option>';
 }
    }

    // Render Domain Relay Table
    if (tbody) {
      tbody.innerHTML = '';
      if (!data.domains || data.domains.length === 0) {
        tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;color:var(--text-dim);padding:18px;">Belum ada domain mail yang terdaftar di sistem. <button class="btn btn-sm btn-primary" onclick="openMailSetupModal()" style="margin-left:8px;padding:3px 10px;font-size:11.5px;">+ Setup Domain Sekarang</button></td></tr>`;
        return;
      }

      data.domains.forEach(d => {
        const relay = data.domain_relays && data.domain_relays[d.domain];
        const tr = document.createElement('tr');

 const globalRelay = data.global_relay;
 const effective = (data.outbound_routes && data.outbound_routes[d.domain]) || data.outbound_default || '';
 const badgeFor = (r, label) => {
 const prov = (r.provider || 'custom').toUpperCase();
 const color = prov === 'RESEND' ? 'rgba(59,130,246,0.18);color:var(--primary);' :
 prov === 'BREVO' ? 'rgba(16,185,129,0.18);color:var(--success);' :
 'rgba(245,158,11,0.18);color:var(--warning);';
 return `<span class="stat-badge" style="background:${color}font-size:11px;font-weight:600;">● ${label}${prov}</span>`;
 };

 let providerBadge = '<span class="stat-badge" style="background:rgba(239,68,68,0.18);color:var(--danger);font-size:11px;font-weight:600;">● BELUM ADA RELAY</span>';
 let hostPort = '<span style="color:var(--danger);font-size:12px;">Outbound nonaktif</span>';
 let userStr = '<span style="color:var(--text-dim);font-size:12px;">-</span>';

 if (relay && relay.host) {
 providerBadge = badgeFor(relay, '');
 hostPort = `<span style="font-family:monospace;font-size:12px;" title="route: ${effective || '-'}">${relay.host}:${relay.port}</span>`;
 userStr = `<span style="font-size:12px;color:var(--text-main);">${relay.username || '-'}</span><div style="font-size:10.5px;color:var(--text-dim);">override domain</div>`;
 } else if (globalRelay && globalRelay.host) {
 providerBadge = badgeFor(globalRelay, 'DEFAULT · ');
 hostPort = `<span style="font-family:monospace;font-size:12px;" title="route: ${effective || '-'}">${globalRelay.host}:${globalRelay.port}</span>`;
 userStr = `<span style="font-size:12px;color:var(--text-main);">${globalRelay.username || '-'}</span><div style="font-size:10.5px;color:var(--text-dim);">ikut relay default</div>`;
 }

        tr.innerHTML = `
          <td><strong>${d.domain}</strong></td>
          <td>${providerBadge}</td>
          <td>${hostPort}</td>
          <td>${userStr}</td>
          <td>
            <a href="${d.admin_url}" target="_blank" rel="noopener noreferrer" class="btn btn-sm btn-outline" style="padding:2px 8px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;">
              <span>Panel Admin</span>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
            </a>
          </td>
          <td>
            <a href="${d.webmail}" target="_blank" rel="noopener noreferrer" class="btn btn-sm btn-outline" style="padding:2px 8px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;">
              <span>Webmail</span>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
            </a>
          </td>
          <td>
            <div style="display:flex;gap:6px;flex-wrap:wrap;">
              <button class="btn btn-sm btn-primary" style="padding:4px 10px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;" onclick="openMailRelayModal('${d.domain}')" title="Setel outbound SMTP relay domain ${d.domain}">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
                <span>${relay && relay.host ? 'Ubah Relay' : 'Setel Relay'}</span>
              </button>
 ${relay && relay.host ? `<button class="btn btn-sm btn-outline" style="padding:4px 10px;font-size:11.5px;color:var(--danger);border-color:rgba(239,68,68,0.35);" onclick="removeMailRelay('${d.domain}')" title="Hapus override relay; domain memakai relay default"><span>Hapus Override</span></button>` : ''}
              <button class="btn btn-sm btn-outline" style="padding:4px 10px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;" onclick="resetMailAdmin('${d.domain}')" title="Reset password admin domain ${d.domain}">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 2l-2 2m-1.5 1.5L16 7m-1.5 1.5L13 10m-1.5 1.5L10 13m-1.5 1.5L7 16m-4 5a4 4 0 1 1 5.66-5.66l9.9-9.9a2 2 0 0 1 2.83 0l1.41 1.41a2 2 0 0 1 0 2.83L12.9 19.66A4 4 0 0 1 7 21z"/></svg>
                <span>Reset Admin</span>
              </button>
 <button class="btn btn-sm btn-outline" style="padding:4px 10px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;" onclick="repairMailDomain('${d.domain}')" title="Perbaiki NS/MX/relay domain ${d.domain}">
 <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>
 <span>Perbaiki</span>
 </button>
 <button class="btn btn-sm btn-outline" style="padding:4px 10px;font-size:11.5px;color:var(--danger);border-color:rgba(239,68,68,0.35);display:inline-flex;align-items:center;gap:4px;" onclick="openMailDomainDeleteModal('${d.domain}')" title="Hapus domain mail ${d.domain}">
 <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/></svg>
 <span>Hapus</span>
 </button>
            </div>
          </td>
        `;
        tbody.appendChild(tr);
      });
    }
  } catch (err) {
    console.error('Failed to load mail status:', err);
    if (tbody) tbody.innerHTML = `<tr><td colspan="7" style="text-align:center;color:var(--text-dim);padding:14px;">Kesalahan jaringan: ${err}</td></tr>`;
  }
}

async function loadMailAccounts() {
  const tbody = document.getElementById('mail-accounts-tbody');
  const summaryEl = document.getElementById('mail-summary-counts');
  if (tbody) tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:var(--text-dim);padding:14px;">Memuat daftar akun email...</td></tr>`;

  try {
    const res = await fetch('/api/mail/accounts');
    if (!res.ok) {
      const errData = await res.json().catch(() => ({}));
      if (tbody) tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:var(--text-dim);padding:14px;">Gagal memuat akun: ${errData.error || res.statusText}</td></tr>`;
      return;
    }
    const data = await res.json();
    cachedMailDomainAccounts = data || [];
    filterMailAccounts();

    // Calculate total accounts
    let totalAccs = 0;
    cachedMailDomainAccounts.forEach(grp => {
      totalAccs += (grp.accounts || []).length;
    });
    if (summaryEl) {
      summaryEl.textContent = `${cachedMailDomainAccounts.length} Domain, ${totalAccs} Akun`;
    }
  } catch (err) {
    console.error('Failed to load mail accounts:', err);
    if (tbody) tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:var(--text-dim);padding:14px;">Kesalahan jaringan: ${err}</td></tr>`;
  }
}

function filterMailAccounts() {
  const tbody = document.getElementById('mail-accounts-tbody');
  const filterVal = document.getElementById('mail-domain-filter')?.value || '';
  if (!tbody) return;

  tbody.innerHTML = '';
  let accountsFound = 0;

  cachedMailDomainAccounts.forEach(grp => {
    if (filterVal && grp.domain !== filterVal) return;

    const relayInfo = grp.relay && grp.relay.host
      ? `<span class="stat-badge" style="background:rgba(59,130,246,0.18);color:var(--primary);font-size:11px;">${grp.relay.provider.toUpperCase()} (${grp.relay.host})</span>`
      : '<span class="stat-badge" style="background:rgba(107,114,128,0.18);color:var(--text-muted);font-size:11px;">Relay Default</span>';

    (grp.accounts || []).forEach(acc => {
      accountsFound++;
      const tr = document.createElement('tr');
      const roleBadge = acc.is_admin
        ? '<span class="stat-badge" style="background:rgba(239,68,68,0.18);color:var(--danger);font-size:11px;font-weight:600;">👑 Administrator</span>'
        : '<span class="stat-badge" style="background:rgba(16,185,129,0.18);color:var(--success);font-size:11px;">Kotak Pos User</span>';

      tr.innerHTML = `
        <td><strong>${grp.domain}</strong></td>
        <td>
          <div style="font-weight:600;color:var(--text-main);">${acc.email}</div>
        </td>
        <td><span style="font-family:monospace;font-size:12px;color:var(--text-muted);">${acc.name || acc.id}</span></td>
        <td>${roleBadge}</td>
        <td>${relayInfo}</td>
        <td>
          <div style="display:flex;gap:6px;">
            <a href="${grp.webmail}" target="_blank" rel="noopener noreferrer" class="btn btn-sm btn-outline" style="padding:2px 8px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;">
              <span>Login Webmail</span>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
            </a>
 ${acc.is_admin ? '' : `<button class="btn btn-sm btn-outline" style="padding:2px 8px;font-size:11.5px;color:var(--danger);border-color:rgba(239,68,68,0.35);" onclick="deleteMailAccount('${acc.email}')" title="Hapus akun ${acc.email}">Hapus</button>`}
          </div>
        </td>
      `;
      tbody.appendChild(tr);
    });
  });

  if (accountsFound === 0) {
    tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:var(--text-dim);padding:14px;">Tidak ada akun email yang cocok dengan filter.</td></tr>`;
  }
}

async function deleteMailAccount(email) {
 const confirmed = await modalConfirm(`Hapus akun email ${email}? Semua email di kotak pos tersebut akan ikut terhapus.`, {
 title: 'Hapus Akun Email', confirmText: 'Hapus Akun', danger: true
 });
 if (!confirmed) return;
 try {
 const res = await fetch('/api/mail/account/delete', {
 method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email })
 });
 const data = await res.json().catch(() => ({}));
 if (!res.ok) {
 await modalAlert(`Gagal menghapus akun: ${data.error || res.statusText}`, 'error', 'Mail Server');
 return;
 }
 await modalAlert(data.message || 'Akun dihapus.', 'success', 'Akun Dihapus');
 loadMail();
 } catch (err) {
 await modalAlert('Error: ' + err, 'error', 'Mail Server');
 }
}

// Hapus / perbaiki domain mail
function openMailDomainDeleteModal(domain) {
 const modal = document.getElementById('modal-mail-domain-delete');
 const nameEl = document.getElementById('mail-domain-delete-name');
 const inputEl = document.getElementById('mail-domain-delete-confirm');
 const dnsEl = document.getElementById('mail-domain-delete-dns');
 if (nameEl) nameEl.textContent = domain || '';
 if (inputEl) inputEl.value = '';
 if (dnsEl) dnsEl.checked = false;
 if (modal) { modal.dataset.domain = domain || ''; modal.classList.add('active'); }
 setTimeout(() => { if (inputEl) inputEl.focus(); }, 50);
}

function closeMailDomainDeleteModal() {
 const modal = document.getElementById('modal-mail-domain-delete');
 if (modal) modal.classList.remove('active');
}

async function submitMailDomainDelete(e) {
 if (e) e.preventDefault();
 const modal = document.getElementById('modal-mail-domain-delete');
 const domain = modal?.dataset.domain || '';
 const typed = document.getElementById('mail-domain-delete-confirm')?.value.trim() || '';
 const deleteDNS = !!document.getElementById('mail-domain-delete-dns')?.checked;
 const btn = document.getElementById('btn-submit-mail-domain-delete');

 if (typed !== domain) {
 await modalAlert('Ketik nama domain persis sama untuk konfirmasi penghapusan.', 'warning', 'Konfirmasi Tidak Cocok');
 return;
 }

 if (btn) {
 btn.disabled = true;
 btn.textContent = 'Menghapus...';
 }
 try {
 const res = await fetch('/api/mail/domain/delete', {
 method: 'POST',
 headers: { 'Content-Type': 'application/json' },
 body: JSON.stringify({ domain, delete_dns: deleteDNS })
 });
 const data = await res.json().catch(() => ({}));
 if (!res.ok) {
 await modalAlert(`Gagal menghapus domain: ${data.error || res.statusText}`, 'error', 'Hapus Domain Mail');
 return;
 }
 const checks = (data.checks || []).map(c => `• [${String(c.status).toUpperCase()}] ${c.name}: ${c.detail}`).join('\n');
 closeMailDomainDeleteModal();
 await modalAlert(`Domain ${domain} dihapus.\n\n${checks || ''}`, data.ok === false ? 'warning' : 'success', 'Domain Mail Dihapus');
 loadMail();
 } catch (err) {
 await modalAlert('Error: ' + err, 'error', 'Hapus Domain Mail');
 } finally {
 if (btn) {
 btn.disabled = false;
 btn.textContent = 'Hapus Domain';
 }
 }
}

async function repairMailDomain(domain) {
 const confirmed = await modalConfirm(
 `Perbaiki domain mail ${domain}?\n\nTindakan: perbaiki NS/SOA & glue zone, pastikan record A/MX/SPF/DMARC, segarkan vhost VPS, lalu rapikan routing relay outbound.`,
 { title: 'Perbaiki Domain Mail', confirmText: 'Perbaiki Sekarang' }
 );
 if (!confirmed) return;
 try {
 const res = await fetch('/api/mail/domain/repair', {
 method: 'POST',
 headers: { 'Content-Type': 'application/json' },
 body: JSON.stringify({ domain })
 });
 const data = await res.json().catch(() => ({}));
 if (!res.ok) {
 await modalAlert(`Gagal memulai perbaikan: ${data.error || res.statusText}`, 'error', 'Perbaiki Domain Mail');
 return;
 }
 openTerminalModal(`Perbaiki Domain Mail: ${domain}`);
 } catch (err) {
 await modalAlert('Error: ' + err, 'error', 'Perbaiki Domain Mail');
 }
}


// Modal: Add Account
function openMailAddAccountModal(domain) {
  const modal = document.getElementById('modal-mail-add-account');
  const emailInput = document.getElementById('mail-new-email');
  const passInput = document.getElementById('mail-new-password');
  const defaultDom = domain || (cachedMailStatus?.domains?.[0]?.domain || '');
  if (emailInput) {
    emailInput.value = defaultDom ? `@${defaultDom}` : '';
    if (defaultDom) {
      try { emailInput.setSelectionRange(0, 0); } catch (_) {}
    }
  }
  if (passInput) passInput.value = '';
  if (modal) modal.classList.add('active');
  setTimeout(() => { if (emailInput) emailInput.focus(); }, 50);
}

function closeMailAddAccountModal() {
  const modal = document.getElementById('modal-mail-add-account');
  if (modal) modal.classList.remove('active');
}

async function submitMailAddAccount(e) {
  if (e) e.preventDefault();
  const email = document.getElementById('mail-new-email')?.value.trim();
  const password = document.getElementById('mail-new-password')?.value;
  const submitBtn = document.getElementById('btn-submit-mail-account');

  if (!email || !password) {
    await modalAlert('Email dan password wajib diisi.', 'warning', 'Validasi Form');
    return;
  }

 if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
 await modalAlert('Format email tidak valid. Gunakan bentuk user@domain.tld.', 'warning', 'Validasi Form');
 return;
 }

  if (submitBtn) {
    submitBtn.disabled = true;
    submitBtn.textContent = 'Membuat Akun...';
  }

  try {
    const res = await fetch('/api/mail/account', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal menambahkan akun: ${data.error}`, 'error', 'Mail Server');
    } else {
      await modalAlert(`Akun email ${email} berhasil dibuat di Stalwart!`, 'success', 'Akun Email Berhasil Dibuat');
      closeMailAddAccountModal();
      loadMail();
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Mail Server');
  } finally {
    if (submitBtn) {
      submitBtn.disabled = false;
      submitBtn.textContent = 'Buat Akun Email';
    }
  }
}

// Modal: Relay
function openMailRelayModal(domain) {
 const modal = document.getElementById('modal-mail-relay');
 const domSelect = document.getElementById('mail-relay-domain');
 if (domSelect && domain) {
 domSelect.value = domain;
 }
 prefillMailRelay(domain);
 toggleMailRelayFields();
 loadMailRelayDNS(domSelect?.value || domain);
 if (modal) modal.classList.add('active');
}

function onMailRelayDomainChange() {
 const domain = document.getElementById('mail-relay-domain')?.value;
 loadMailRelayDNS(domain);
}

function currentMailRelayProvider() {
 return document.getElementById('mail-relay-provider')?.value || '';
}

async function loadMailRelayDNS(domain) {
 const area = document.getElementById('mail-relay-dns-records');
 if (!area) return;
 if (!domain) {
 area.value = '';
 return;
 }
 try {
 const res = await fetch('/api/mail/relay/dns?domain=' + encodeURIComponent(domain));
 const list = res.ok ? await res.json() : [];
 const provider = currentMailRelayProvider();
 area.value = (Array.isArray(list) ? list : [])
 .filter(r => !provider || String(r.provider || '').toLowerCase() === provider)
 .map(r => `${r.type} ${r.name} ${r.content}`)
 .join('\n');
 } catch (_) {
 area.value = '';
 }
}

function parseMailRelayDNS(text) {
 return String(text || '').split('\n').map(line => line.trim()).filter(Boolean).map(line => {
 const parts = line.split(/\s+/);
 if (parts.length < 3) {
 throw new Error('Format baris tidak valid: ' + line);
 }
 return { type: parts[0].toUpperCase(), name: parts[1], content: parts.slice(2).join(' ') };
 });
}

async function saveMailRelayDNS(domain, provider, text) {
 let records = [];
 try {
 records = parseMailRelayDNS(text);
 } catch (err) {
 return { added: [], kept: [], manual: [err.message] };
 }
 if (!records.length) return null;
 try {
 const res = await fetch('/api/mail/relay/dns', {
 method: 'POST',
 headers: { 'Content-Type': 'application/json' },
 body: JSON.stringify({ domain, provider, records })
 });
 const data = await res.json().catch(() => ({}));
 if (!res.ok) {
 return { added: [], kept: [], manual: ['Gagal menyimpan record relay: ' + (data.error || res.statusText)] };
 }
 return data;
 } catch (err) {
 return { added: [], kept: [], manual: ['Gagal menyimpan record relay: ' + err.message] };
 }
}

function prefillMailRelay(domain) {
 const data = cachedMailStatus || {};
 const relay = domain === 'global'
 ? data.global_relay
 : (data.domain_relays && data.domain_relays[domain]) || data.global_relay;
 const provSel = document.getElementById('mail-relay-provider');
 const secret = document.getElementById('mail-relay-secret');
 const host = document.getElementById('mail-relay-host');
 const port = document.getElementById('mail-relay-port');
 const user = document.getElementById('mail-relay-user');
 const pass = document.getElementById('mail-relay-pass');
 if (secret) secret.value = '';
 if (pass) pass.value = '';
 if (!relay || !relay.host) {
 if (provSel) provSel.value = 'brevo';
 if (host) host.value = '';
 if (user) user.value = '';
 return;
 }
 const prov = String(relay.provider || '').toLowerCase();
 if (provSel && ['resend', 'brevo', 'custom'].includes(prov)) {
 provSel.value = prov;
 }
 if (host) host.value = relay.host || '';
 if (port) port.value = relay.port || 587;
 if (user) user.value = relay.username || '';
}

async function removeMailRelay(domain) {
 const confirmed = await modalConfirm(`Hapus override relay untuk ${domain}? Domain akan memakai relay default (bukan direct).`, {
 title: 'Hapus Override Relay',
 confirmText: 'Hapus Override',
 danger: true
 });
 if (!confirmed) return;
 try {
 const res = await fetch('/api/mail/relay/remove', {
 method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ domain })
 });
 const data = await res.json().catch(() => ({}));
 if (!res.ok) {
 await modalAlert(`Gagal menghapus relay: ${data.error || res.statusText}`, 'error', 'Mail Server');
 return;
 }
 await modalAlert(relayDNSMessage(domain, data.dns).replace('berhasil disimpan', 'dihapus'), relayDNSHasIssue(data.dns) ? 'warning' : 'success', 'Relay Dihapus');
 loadMail();
 } catch (err) {
 await modalAlert('Error: ' + err, 'error', 'Mail Server');
 }
}

function closeMailRelayModal() {
  const modal = document.getElementById('modal-mail-relay');
  if (modal) modal.classList.remove('active');
}

function toggleMailRelayFields() {
  const prov = document.getElementById('mail-relay-provider')?.value || 'resend';
  const groupSecret = document.getElementById('mail-relay-group-secret');
  const groupCustom = document.getElementById('mail-relay-group-custom');
  const secretLabel = document.getElementById('mail-relay-secret-label');
  const secretHelp = document.getElementById('mail-relay-secret-help');
  const secretInput = document.getElementById('mail-relay-secret');

  if (prov === 'resend') {
    if (groupSecret) groupSecret.style.display = 'block';
    if (groupCustom) groupCustom.style.display = 'none';
    if (secretLabel) secretLabel.textContent = 'API Key Resend (re_...)';
    if (secretHelp) secretHelp.textContent = 'Port 465 (Implicit TLS). Username otomatis disetel ke "resend".';
    if (secretInput) secretInput.placeholder = 're_123456789...';
  } else if (prov === 'brevo') {
    if (groupSecret) groupSecret.style.display = 'block';
    if (groupCustom) groupCustom.style.display = 'none';
    if (secretLabel) secretLabel.textContent = 'Master SMTP Key Brevo (xkeysib-...)';
    if (secretHelp) secretHelp.textContent = 'Port 587 (STARTTLS). Username otomatis diambil dari login akun Brevo Anda.';
    if (secretInput) secretInput.placeholder = 'xkeysib-123456789...';
  } else if (prov === 'custom') {
    if (groupSecret) groupSecret.style.display = 'none';
    if (groupCustom) groupCustom.style.display = 'block';
  } else {
    // direct
    if (groupSecret) groupSecret.style.display = 'none';
    if (groupCustom) groupCustom.style.display = 'none';
  }
}

function relayDNSHasIssue(dns) {
 if (!dns) return false;
 return (dns.manual || []).length > 0;
}

function relayDNSMessage(domain, dns) {
 let msg = `Konfigurasi outbound SMTP relay domain ${domain} berhasil disimpan.`;
 if (!dns) {
 msg += '\n\nRecord DNS relay tidak berubah (belum ada record terkelola).';
 return msg;
 }
 const lines = [];
 if ((dns.removed || []).length) {
 lines.push(`Dihapus otomatis (${dns.previous_provider || 'provider lama'}):`);
 dns.removed.forEach(r => lines.push('  - ' + r));
 }
 if ((dns.added || []).length) {
 lines.push(`Dipasang otomatis (${dns.provider || 'provider baru'}):`);
 dns.added.forEach(r => lines.push('  - ' + r));
 }
 if ((dns.kept || []).length) {
 lines.push('Dipertahankan:');
 dns.kept.forEach(r => lines.push('  - ' + r));
 }
 if ((dns.manual || []).length) {
 lines.push('Perlu dipasang manual:');
 dns.manual.forEach(r => lines.push('  - ' + r));
 }
 if (lines.length) msg += '\n\n' + lines.join('\n');
 return msg;
}


async function submitMailRelay(e) {
  if (e) e.preventDefault();
  const domain = document.getElementById('mail-relay-domain')?.value.trim();
  const provider = document.getElementById('mail-relay-provider')?.value || 'resend';
  const secret = document.getElementById('mail-relay-secret')?.value.trim();
  const host = document.getElementById('mail-relay-host')?.value.trim();
  const port = parseInt(document.getElementById('mail-relay-port')?.value, 10) || 587;
  const user = document.getElementById('mail-relay-user')?.value.trim();
  const pass = document.getElementById('mail-relay-pass')?.value;
  const submitBtn = document.getElementById('btn-submit-mail-relay');

  if (!domain) {
    await modalAlert('Pilih domain target terlebih dahulu.', 'warning', 'Validasi Form');
    return;
  }

  const payload = { domain, provider };
  if (provider === 'resend' || provider === 'brevo') {
    if (!secret) {
      await modalAlert(`API Key / SMTP Key untuk ${provider.toUpperCase()} wajib diisi.`, 'warning', 'Validasi Form');
      return;
    }
    payload.password = secret;
  } else if (provider === 'custom') {
    if (!host) {
      await modalAlert('Host SMTP wajib diisi untuk relay kustom.', 'warning', 'Validasi Form');
      return;
    }
    payload.host = host;
    payload.port = port;
    payload.username = user;
    payload.password = pass;
  }

  if (submitBtn) {
    submitBtn.disabled = true;
    submitBtn.textContent = 'Menyimpan Relay...';
  }

  try {
    const res = await fetch('/api/mail/relay', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal menyetel relay: ${data.error}`, 'error', 'Mail Server');
    } else {
  const dnsText = document.getElementById('mail-relay-dns-records')?.value || '';
  const saved = await saveMailRelayDNS(domain, provider, dnsText);
  let dnsReport = data.dns;
  if (saved) {
  const base = dnsReport || {};
  dnsReport = Object.assign({}, base, {
  added: (saved.added || []).concat(base.added || []),
  kept: (saved.kept || []).concat(base.kept || []),
  manual: (saved.manual || []).concat(base.manual || [])
  });
  }
 closeMailRelayModal();
 await modalAlert(relayDNSMessage(domain, dnsReport), relayDNSHasIssue(dnsReport) ? 'warning' : 'success', 'Relay Berhasil Disetel');
 loadMail();
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Mail Server');
  } finally {
    if (submitBtn) {
      submitBtn.disabled = false;
      submitBtn.textContent = 'Simpan Konfigurasi';
    }
  }
}

async function resetMailAdmin(domain) {
  const confirmed = await modalConfirm(`Reset password admin Stalwart untuk domain '${domain}'?\nPassword baru yang acak dan aman akan dibuat otomatis dan disimpan di server.`, {
    title: 'Reset Password Admin',
    confirmText: 'Reset Password',
    type: 'warning'
  });
  if (!confirmed) return;

  try {
    const res = await fetch('/api/mail/admin-reset', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ domain })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal reset admin: ${data.error}`, 'error', 'Mail Server');
    } else {
      await modalAlert(`Password admin untuk domain ${domain} berhasil direset! Kredensial tersimpan di /etc/wdev/mail-admin.cred.`, 'success', 'Admin Berhasil Direset');
      loadMail();
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Mail Server');
  }
}

// Modal: Setup Domain & Mail Server
async function openMailSetupModal(defaultDomain) {
  const modal = document.getElementById('modal-mail-setup');
  if (modal) {
    onMailSetupRelayChange();
    modal.classList.add('active');
    await populateMailSetupDomainOptions(defaultDomain);
 refreshMailPreflight();
  }
}

async function refreshMailPreflight() {
 const box = document.getElementById('mail-setup-preflight');
 if (!box) return;
 const sel = document.getElementById('mail-setup-domain-select');
 let domain = sel ? sel.value.trim() : '';
 if (domain === '__custom__') {
 domain = document.getElementById('mail-setup-domain-custom')?.value.trim() || '';
 }
 box.style.display = 'block';
 box.style.background = 'rgba(107,114,128,0.12)';
 box.style.color = 'var(--text-muted)';
 box.textContent = 'Memeriksa prasyarat mail...';
 try {
 const res = await fetch('/api/mail/preflight?domain=' + encodeURIComponent(domain || ''));
 if (!res.ok) {
 box.textContent = 'Gagal memeriksa prasyarat mail.';
 return;
 }
 const p = await res.json();
 const rows = [];
 if (p.blockers && p.blockers.length) {
 box.style.background = 'rgba(239,68,68,0.10)';
 box.style.color = 'var(--text-main)';
 rows.push('<strong style="color:var(--danger);">Blocker (setup akan gagal):</strong>');
 p.blockers.forEach(b => rows.push('• ' + b));
 }
 if (p.warnings && p.warnings.length) {
 if (!(p.blockers && p.blockers.length)) {
 box.style.background = 'rgba(245,158,11,0.10)';
 box.style.color = 'var(--text-main)';
 }
 rows.push('<strong style="color:var(--warning);">Peringatan:</strong>');
 p.warnings.forEach(w => rows.push('• ' + w));
 }
 if (!rows.length) {
 box.style.background = 'rgba(16,185,129,0.10)';
 box.style.color = 'var(--text-main)';
 rows.push('<strong style="color:var(--success);">Prasyarat lengkap.</strong> Stalwart, CLI, NetBird, Edge Gateway, dan relay default siap.');
 }
 box.innerHTML = rows.join('<br>');
 } catch (err) {
 box.textContent = 'Gagal memeriksa prasyarat: ' + err;
 }
}

async function populateMailSetupDomainOptions(defaultDomain) {
  const select = document.getElementById('mail-setup-domain-select');
  const customWrap = document.getElementById('mail-setup-custom-domain-wrap');
  const customInput = document.getElementById('mail-setup-domain-custom');
  const warnBox = document.getElementById('mail-setup-domain-warning');
  if (!select) return;

  select.innerHTML = '<option value="">Memeriksa domain yang tersedia...</option>';
  if (customWrap) customWrap.style.display = 'none';
  if (warnBox) warnBox.style.display = 'none';

  try {
    // 1. Ambil zone DNS aktif dari PowerDNS VPS Gateway
    let zones = cachedZones;
    if (!zones || zones.length === 0) {
      const res = await fetch('/api/pdns/zones');
      if (res.ok) {
        zones = await res.json() || [];
        cachedZones = zones;
      }
    }

    // 2. Ambil status mail terkini untuk mengetahui domain yang sudah terkonfigurasi
    let configuredDomains = [];
    if (cachedMailStatus && Array.isArray(cachedMailStatus.domains)) {
      configuredDomains = cachedMailStatus.domains.map(d => d.domain);
    } else {
      const resMail = await fetch('/api/mail/status');
      if (resMail.ok) {
        const mailData = await resMail.json();
        cachedMailStatus = mailData;
        configuredDomains = (mailData.domains || []).map(d => d.domain);
      }
    }

    // Ekstrak nama domain unik (bersihkan trailing dot)
    const availableDomains = [];
    (zones || []).forEach(z => {
      const cleanName = (z.name || '').replace(/\.$/, '').trim();
      if (cleanName && !availableDomains.includes(cleanName)) {
        availableDomains.push(cleanName);
      }
    });

    if (availableDomains.length === 0) {
      // Tidak ada zone terdeteksi di PowerDNS
      select.innerHTML = '<option value="__custom__">-- Masukkan Domain Manual (Belum ada zone) --</option>';
      if (customWrap) customWrap.style.display = 'block';
      if (warnBox) warnBox.style.display = 'block';
      if (customInput && defaultDomain) customInput.value = defaultDomain;
      return;
    }

    // Pisahkan antara domain yang belum di-setup vs sudah di-setup
    const readyDomains = availableDomains.filter(d => !configuredDomains.includes(d));
    const alreadyConfigured = availableDomains.filter(d => configuredDomains.includes(d));

    let html = '';
    if (readyDomains.length > 0) {
      html += `<optgroup label="Domain Tersedia di PowerDNS (Siap Setup)">`;
      readyDomains.forEach(d => {
        html += `<option value="${d}">${d} (Tersedia • Siap Setup)</option>`;
      });
      html += `</optgroup>`;
    }

    if (alreadyConfigured.length > 0) {
      html += `<optgroup label="Domain Terdaftar di PowerDNS (Sudah Terpasang)">`;
      alreadyConfigured.forEach(d => {
        html += `<option value="${d}">${d} (Sudah Terpasang • Setup Ulang)</option>`;
      });
      html += `</optgroup>`;
    }

    html += `<optgroup label="Opsi Lainnya">
      <option value="__custom__">+ Masukkan Domain Kustom / Manual...</option>
    </optgroup>`;

    select.innerHTML = html;

    // Tentukan seleksi target otomatis
    let target = '';
    if (defaultDomain && availableDomains.includes(defaultDomain)) {
      target = defaultDomain;
    } else if (readyDomains.length > 0) {
      target = readyDomains[0];
    } else if (alreadyConfigured.length > 0) {
      target = alreadyConfigured[0];
    } else if (defaultDomain) {
      target = '__custom__';
    }

    if (target) {
      select.value = target;
      if (target === '__custom__') {
        if (customWrap) customWrap.style.display = 'block';
        if (customInput) customInput.value = defaultDomain || '';
      }
    }
  } catch (err) {
    console.error('Gagal memuat opsi domain setup mail:', err);
    select.innerHTML = '<option value="__custom__">+ Masukkan Domain Manual</option>';
    if (customWrap) customWrap.style.display = 'block';
    if (customInput && defaultDomain) customInput.value = defaultDomain;
  }
}

function onMailSetupDomainSelectChange() {
  const select = document.getElementById('mail-setup-domain-select');
  const customWrap = document.getElementById('mail-setup-custom-domain-wrap');
  const customInput = document.getElementById('mail-setup-domain-custom');
  if (!select) return;
 refreshMailPreflight();

  if (select.value === '__custom__') {
    if (customWrap) customWrap.style.display = 'block';
    if (customInput) customInput.focus();
  } else {
    if (customWrap) customWrap.style.display = 'none';
  }
}

function closeMailSetupModal() {
  const modal = document.getElementById('modal-mail-setup');
  if (modal) modal.classList.remove('active');
}

function onMailSetupRelayChange() {
  const prov = document.getElementById('mail-setup-relay-prov')?.value || '';
  const grpResend = document.getElementById('mail-setup-grp-resend');
  const grpBrevo = document.getElementById('mail-setup-grp-brevo');
  const grpCustom = document.getElementById('mail-setup-grp-custom');

  if (grpResend) grpResend.style.display = prov === 'resend' ? 'block' : 'none';
  if (grpBrevo) grpBrevo.style.display = prov === 'brevo' ? 'block' : 'none';
  if (grpCustom) grpCustom.style.display = prov === 'custom' ? 'block' : 'none';
}

async function submitMailSetup(e) {
  if (e) e.preventDefault();
  const select = document.getElementById('mail-setup-domain-select');
  let domain = select ? select.value.trim() : '';
  if (domain === '__custom__') {
    domain = document.getElementById('mail-setup-domain-custom')?.value.trim() || '';
  }
  if (!domain) {
    domain = document.getElementById('mail-setup-domain')?.value.trim() || '';
  }
  const ssl = document.getElementById('mail-setup-ssl')?.checked ?? true;
  const relayProv = document.getElementById('mail-setup-relay-prov')?.value || '';

  if (!domain) {
    await modalAlert('Silakan pilih atau masukkan domain publik yang valid.', 'warning', 'Validasi Input');
    return;
  }

  let relayHost = '';
  let relayPort = 587;
  let relayUser = '';
  let relayPass = '';

  if (!relayProv) {
 // warisi relay default; backend menolak jika belum ada
 } else if (relayProv === 'resend') {
    relayPass = document.getElementById('mail-setup-resend-key')?.value.trim() || '';
    if (!relayPass) {
      await modalAlert('API Key Resend wajib diisi jika memilih relay Resend.', 'warning', 'Validasi Input');
      return;
    }
  } else if (relayProv === 'brevo') {
    relayUser = document.getElementById('mail-setup-brevo-user')?.value.trim() || '';
    relayPass = document.getElementById('mail-setup-brevo-key')?.value.trim() || '';
    if (!relayPass) {
      await modalAlert('SMTP Key Brevo wajib diisi jika memilih relay Brevo.', 'warning', 'Validasi Input');
      return;
    }
  } else if (relayProv === 'custom') {
    relayHost = document.getElementById('mail-setup-custom-host')?.value.trim() || '';
    relayPort = parseInt(document.getElementById('mail-setup-custom-port')?.value, 10) || 587;
    relayUser = document.getElementById('mail-setup-custom-user')?.value.trim() || '';
    relayPass = document.getElementById('mail-setup-custom-pass')?.value || '';
    if (!relayHost) {
      await modalAlert('SMTP Host wajib diisi untuk Custom Relay.', 'warning', 'Validasi Input');
      return;
    }
  }

  const confirmed = await modalConfirm(`Mulai setup otomatis Mail Server & DNS untuk domain "${domain}"?\nProses ini akan mengonfigurasi engine Stalwart lokal, SnappyMail, DNS record di VPS, dan sertifikat SSL.`, {
    title: 'Konfirmasi Setup Mail Server',
    confirmText: 'Mulai Setup'
  });
  if (!confirmed) return;

  const submitBtn = document.getElementById('btn-submit-mail-setup');
  if (submitBtn) {
    submitBtn.disabled = true;
    submitBtn.textContent = 'Memulai Setup...';
  }

  try {
    const res = await fetch('/api/mail/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        domain: domain,
        ssl: ssl,
        relay_provider: relayProv,
        relay_host: relayHost,
        relay_port: relayPort,
        relay_user: relayUser,
        relay_pass: relayPass
      })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal memulai setup: ${data.error || 'Terjadi kesalahan'}`, 'error', 'Setup Mail');
      return;
    }
    closeMailSetupModal();
    openTerminalModal(`Setup Mail Server: ${domain}`);
  } catch (err) {
    await modalAlert('Kesalahan koneksi: ' + err, 'error', 'Setup Mail');
  } finally {
    if (submitBtn) {
      submitBtn.disabled = false;
      submitBtn.textContent = 'Mulai Setup Domain';
    }
  }
}

// Dismiss modals / tooltips on Escape key (WCAG 1.4.13)
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    if (document.activeElement && document.activeElement.hasAttribute('data-tooltip')) {
      document.activeElement.blur();
    }
    document.querySelectorAll('.modal-overlay.active').forEach(modal => {
      modal.classList.remove('active');
    });
  }
});

// Close any modal overlay when clicking outside on the backdrop
document.addEventListener('click', (e) => {
  if (e.target && e.target.classList && e.target.classList.contains('modal-overlay')) {
    e.target.classList.remove('active');
  }
});

// ==========================================
// SERVICE CONFIG DIALOGS (Redis, PostgreSQL, PHP-FPM)
// ==========================================
function initServiceConfigDialogs() {
  const dialogs = document.querySelectorAll('dialog.service-config-dialog');
  dialogs.forEach((dialog) => {
    dialog.addEventListener('close', () => {
      dialog.returnValue = '';
    });
  });

  // Navigasi keyboard roving tabindex untuk tablist di dalam modal
  document.querySelectorAll('.modal-tablist[role="tablist"]').forEach((tablist) => {
    tablist.addEventListener('keydown', (e) => {
      const tabs = Array.from(tablist.querySelectorAll('[role="tab"]'));
      const idx = tabs.indexOf(document.activeElement);
      if (idx === -1) return;
      let nextIdx = -1;
      if (e.key === 'ArrowRight') nextIdx = (idx + 1) % tabs.length;
      else if (e.key === 'ArrowLeft') nextIdx = (idx - 1 + tabs.length) % tabs.length;
      else if (e.key === 'Home') nextIdx = 0;
      else if (e.key === 'End') nextIdx = tabs.length - 1;
      if (nextIdx !== -1) {
        e.preventDefault();
        tabs[nextIdx].focus();
      }
    });
  });

  // Fallback light dismiss untuk browser tanpa dukungan HTMLDialogElement.prototype.closedBy
  if (!('closedBy' in HTMLDialogElement.prototype)) {
    const isOutside = (e, dialog) => {
      const r = dialog.getBoundingClientRect();
      return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
    };
    for (const dialog of document.querySelectorAll('dialog.service-config-dialog[closedby="any"]')) {
      let pressedOutside = false;
      dialog.addEventListener('pointerdown', (e) => {
        pressedOutside = isOutside(e, dialog);
      });
      dialog.addEventListener('pointerup', (e) => {
        if (pressedOutside && isOutside(e, dialog)) {
          dialog.close();
        }
        pressedOutside = false;
      });
    }
  }
}

function switchConfigModalTab(dialogId, tabKey) {
  const dialog = document.getElementById(dialogId);
  if (!dialog) return;

  const tabs = dialog.querySelectorAll('[role="tab"]');
  tabs.forEach((tab) => {
    const selected = tab.id.endsWith(`-${tabKey}`);
    tab.setAttribute('aria-selected', selected ? 'true' : 'false');
    tab.setAttribute('tabindex', selected ? '0' : '-1');
    if (selected && typeof tab.scrollIntoView === 'function') {
      tab.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }
  });

  const panels = dialog.querySelectorAll('[role="tabpanel"]');
  panels.forEach((panel) => {
    const match = panel.getAttribute('data-tab-panel') === tabKey;
    panel.hidden = !match;
  });

  if (dialogId === 'modal-config-redis') {
    if (tabKey === 'inspect') loadRedisInspect();
    else if (tabKey === 'logs') loadServiceConfigLogs('redis');
  } else if (dialogId === 'modal-config-postgres') {
    if (tabKey === 'inspect') loadPostgresInspect();
    else if (tabKey === 'logs') loadServiceConfigLogs('postgresql');
  } else if (dialogId === 'modal-config-php') {
    if (tabKey === 'logs') loadServiceConfigLogs('php-fpm');
  } else if (dialogId === 'modal-config-nginx') {
    if (tabKey === 'logs') loadServiceConfigLogs('nginx');
  }
}

function closeServiceConfigDialog(dialogId) {
  const dialog = document.getElementById(dialogId);
  if (dialog && dialog.open) {
    dialog.close();
  }
}

function setConfigInlineAlert(elementId, message, type = 'error') {
  const el = document.getElementById(elementId);
  if (!el) return;
  if (!message) {
    el.textContent = '';
    el.className = 'config-inline-alert';
    return;
  }
  el.textContent = message;
  el.className = `config-inline-alert visible ${type}`;
}

async function promptAndVerifySudo(message) {
  let promptMsg = message || 'Operasi konfigurasi ini memerlukan hak akses administrator (sudo).\nSilakan masukkan password sudo Anda:';
  while (true) {
    const pass = await modalPrompt(promptMsg, {
      title: 'Akses Sudo Diperlukan',
      placeholder: 'Masukkan password sudo...',
      confirmText: 'Verifikasi',
      cancelText: 'Batal',
      isPassword: true
    });
    if (!pass) return false;
    const res = await fetch('/api/sudo/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: pass })
    });
    if (res.ok) return true;
    promptMsg = 'Password sudo tidak valid. Silakan masukkan kembali password sudo Anda:';
  }
}

async function fetchServiceConfig(serviceName, version = '') {
  const qs = version ? `?version=${encodeURIComponent(version)}` : '';
  while (true) {
    const res = await fetch(`/api/service-config/${encodeURIComponent(serviceName)}${qs}`);
    const data = await res.json().catch(() => ({}));
    if (data.status === 'sudo_required') {
      const ok = await promptAndVerifySudo(data.message);
      if (!ok) return null;
      continue;
    }
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }
    return data.config;
  }
}

async function postServiceConfig(serviceName, payload) {
  const bodyPayload = { ...payload };
  let promptMsg = 'Menyimpan konfigurasi & me-restart layanan memerlukan hak akses sudo.\nSilakan masukkan password sudo Anda:';

  while (true) {
    const res = await fetch(`/api/service-config/${encodeURIComponent(serviceName)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(bodyPayload)
    });
    const data = await res.json().catch(() => ({}));
    if (data.status === 'sudo_required' || res.status === 401) {
      if (res.status === 401) {
        promptMsg = 'Password sudo tidak valid. Silakan masukkan kembali password sudo Anda:';
      }
      const pass = await modalPrompt(promptMsg, {
        title: 'Akses Sudo Diperlukan',
        placeholder: 'Masukkan password sudo...',
        confirmText: 'Lanjutkan',
        cancelText: 'Batal',
        isPassword: true
      });
      if (!pass) return null;
      bodyPayload.sudo_pass = pass;
      continue;
    }
    if (!res.ok) {
      throw new Error(data.error || `HTTP ${res.status}`);
    }
    return data.config;
  }
}

async function openServiceConfigModal(serviceName, version = '') {
  try {
    const cfg = await fetchServiceConfig(serviceName, version);
    if (!cfg) return;

    if (serviceName === 'redis') {
      populateRedisConfigForm(cfg);
      switchConfigModalTab('modal-config-redis', 'settings');
      const dialog = document.getElementById('modal-config-redis');
      if (dialog && !dialog.open) dialog.showModal();
    } else if (serviceName === 'postgresql' || serviceName === 'postgres') {
      populatePostgresConfigForm(cfg);
      switchConfigModalTab('modal-config-postgres', 'settings');
      const dialog = document.getElementById('modal-config-postgres');
      if (dialog && !dialog.open) dialog.showModal();
    } else if (serviceName === 'php-fpm' || serviceName === 'php') {
      populatePHPConfigForm(cfg);
      switchConfigModalTab('modal-config-php', 'settings');
      const dialog = document.getElementById('modal-config-php');
      if (dialog && !dialog.open) dialog.showModal();
    } else if (serviceName === 'nginx') {
      populateNginxConfigForm(cfg);
      switchConfigModalTab('modal-config-nginx', 'settings');
      const dialog = document.getElementById('modal-config-nginx');
      if (dialog && !dialog.open) dialog.showModal();
    }
  } catch (err) {
    await modalAlert(`Gagal memuat konfigurasi ${serviceName}: ${err.message}`, 'error', 'Konfigurasi Layanan');
  }
}

// --- Redis / Valkey ---
function populateRedisConfigForm(cfg) {
  setConfigInlineAlert('redis-cfg-alert', '');
  const meta = document.getElementById('redis-cfg-meta');
  if (meta) {
    meta.textContent = `${cfg.config_path || '-'} • Unit: ${cfg.service_unit || '-'} (${cfg.active ? 'Aktif' : 'Nonaktif'})`;
  }
  document.getElementById('redis-cfg-port').value = cfg.port || 6379;
  document.getElementById('redis-cfg-bind').value = cfg.bind || '127.0.0.1 -::1';
  document.getElementById('redis-cfg-protected').value = cfg.protected_mode || 'yes';
  document.getElementById('redis-cfg-password').value = '';
  document.getElementById('redis-cfg-clear-pass').checked = false;
  const passStatus = document.getElementById('redis-cfg-pass-status');
  if (passStatus) {
    passStatus.textContent = cfg.has_password ? 'Password Aktif (Terpasang)' : 'Tanpa Password';
    passStatus.style.color = cfg.has_password ? 'var(--success)' : 'var(--warning)';
  }
  document.getElementById('redis-cfg-maxmemory').value = cfg.maxmemory || '0';
  document.getElementById('redis-cfg-policy').value = cfg.maxmemory_policy || 'noeviction';
  document.getElementById('redis-cfg-persistence').value = cfg.persistence_mode || 'rdb';
  document.getElementById('redis-cfg-maxclients').value = cfg.maxclients || 10000;
  document.getElementById('redis-cfg-databases').value = cfg.databases || 16;
  document.getElementById('redis-cfg-restart').checked = true;
}

function applyRedisPreset(preset) {
  setConfigInlineAlert('redis-cfg-alert', '');
  if (preset === 'local') {
    document.getElementById('redis-cfg-port').value = 6379;
    document.getElementById('redis-cfg-bind').value = '127.0.0.1 -::1';
    document.getElementById('redis-cfg-protected').value = 'yes';
    document.getElementById('redis-cfg-maxmemory').value = '256mb';
    document.getElementById('redis-cfg-policy').value = 'noeviction';
    document.getElementById('redis-cfg-persistence').value = 'rdb';
  } else if (preset === 'cache') {
    document.getElementById('redis-cfg-maxmemory').value = '256mb';
    document.getElementById('redis-cfg-policy').value = 'allkeys-lru';
    document.getElementById('redis-cfg-persistence').value = 'none';
  } else if (preset === 'queue') {
    document.getElementById('redis-cfg-maxmemory').value = '512mb';
    document.getElementById('redis-cfg-policy').value = 'noeviction';
    document.getElementById('redis-cfg-persistence').value = 'aof';
  }
}

async function submitRedisConfig(event) {
  event.preventDefault();
  setConfigInlineAlert('redis-cfg-alert', '');

  const port = parseInt(document.getElementById('redis-cfg-port').value.trim(), 10);
  const bind = document.getElementById('redis-cfg-bind').value.trim();
  const protectedMode = document.getElementById('redis-cfg-protected').value;
  const password = document.getElementById('redis-cfg-password').value;
  const clearPassword = document.getElementById('redis-cfg-clear-pass').checked;
  const maxmemory = document.getElementById('redis-cfg-maxmemory').value.trim();
  const maxmemoryPolicy = document.getElementById('redis-cfg-policy').value;
  const persistenceMode = document.getElementById('redis-cfg-persistence').value;
  const maxclients = parseInt(document.getElementById('redis-cfg-maxclients').value.trim(), 10);
  const databases = parseInt(document.getElementById('redis-cfg-databases').value.trim(), 10);
  const restart = document.getElementById('redis-cfg-restart').checked;

  if (!port || port < 1 || port > 65535) {
    setConfigInlineAlert('redis-cfg-alert', 'Port harus berada di antara 1 sampai 65535.');
    document.getElementById('redis-cfg-port').focus();
    return;
  }

  const btn = document.getElementById('btn-save-redis-cfg');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';
  }
  try {
    const saved = await postServiceConfig('redis', {
      port,
      bind,
      protected_mode: protectedMode,
      password,
      clear_password: clearPassword,
      maxmemory,
      maxmemory_policy: maxmemoryPolicy,
      persistence_mode: persistenceMode,
      maxclients,
      databases,
      restart
    });
    if (!saved) return;
    closeServiceConfigDialog('modal-config-redis');
    loadStatus();
    await modalAlert('Konfigurasi Redis / Valkey berhasil disimpan dan diterapkan.', 'success', 'Konfigurasi Redis');
  } catch (err) {
    setConfigInlineAlert('redis-cfg-alert', err.message || 'Gagal menyimpan konfigurasi Redis.');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = 'Simpan Konfigurasi';
    }
  }
}

// --- PostgreSQL ---
function populatePostgresConfigForm(cfg) {
  setConfigInlineAlert('pg-cfg-alert', '');
  const meta = document.getElementById('pg-cfg-meta');
  if (meta) {
    meta.textContent = `${cfg.config_path || '-'} • Unit: ${cfg.service_unit || '-'} (${cfg.active ? 'Aktif' : 'Nonaktif'})`;
  }

  const verSelect = document.getElementById('pg-cfg-version');
  if (verSelect) {
    const versions = (cfg.available_versions && cfg.available_versions.length > 0)
      ? cfg.available_versions
      : [cfg.version || '16'];
    verSelect.innerHTML = versions.map(v =>
      `<option value="${escapeHtml(v)}" ${v === cfg.version ? 'selected' : ''}>PostgreSQL ${escapeHtml(v)}</option>`
    ).join('');
  }

  document.getElementById('pg-cfg-port').value = cfg.port || 5432;
  document.getElementById('pg-cfg-listen').value = cfg.listen_addresses || 'localhost';
  document.getElementById('pg-cfg-remote-mode').value = cfg.remote_access_mode || 'local';
  document.getElementById('pg-cfg-auth').value = cfg.password_encryption || 'scram-sha-256';
  document.getElementById('pg-cfg-newpass').value = '';
  document.getElementById('pg-cfg-maxconn').value = cfg.max_connections || 100;
  document.getElementById('pg-cfg-sharedbuf').value = cfg.shared_buffers || '128MB';
  document.getElementById('pg-cfg-effcache').value = cfg.effective_cache_size || '4GB';
  document.getElementById('pg-cfg-workmem').value = cfg.work_mem || '4MB';
  document.getElementById('pg-cfg-maintmem').value = cfg.maintenance_work_mem || '64MB';
  document.getElementById('pg-cfg-timezone').value = cfg.timezone || 'UTC';
  document.getElementById('pg-cfg-slowlog').value = (cfg.log_min_duration_statement !== undefined) ? cfg.log_min_duration_statement : -1;
  document.getElementById('pg-cfg-restart').checked = true;
}

async function onPostgresConfigVersionChange() {
  const ver = document.getElementById('pg-cfg-version')?.value || '';
  try {
    const cfg = await fetchServiceConfig('postgresql', ver);
    if (cfg) populatePostgresConfigForm(cfg);
  } catch (err) {
    setConfigInlineAlert('pg-cfg-alert', `Gagal memuat versi PostgreSQL ${ver}: ${err.message}`);
  }
}

function applyPostgresPreset(preset) {
  setConfigInlineAlert('pg-cfg-alert', '');
  if (preset === 'dev') {
    document.getElementById('pg-cfg-maxconn').value = 50;
    document.getElementById('pg-cfg-sharedbuf').value = '128MB';
    document.getElementById('pg-cfg-effcache').value = '512MB';
    document.getElementById('pg-cfg-workmem').value = '4MB';
    document.getElementById('pg-cfg-maintmem').value = '64MB';
    document.getElementById('pg-cfg-slowlog').value = 250;
  } else if (preset === 'balanced') {
    document.getElementById('pg-cfg-maxconn').value = 150;
    document.getElementById('pg-cfg-sharedbuf').value = '512MB';
    document.getElementById('pg-cfg-effcache').value = '2GB';
    document.getElementById('pg-cfg-workmem').value = '16MB';
    document.getElementById('pg-cfg-maintmem').value = '128MB';
    document.getElementById('pg-cfg-timezone').value = 'Asia/Jakarta';
    document.getElementById('pg-cfg-slowlog').value = 500;
  } else if (preset === 'high') {
    document.getElementById('pg-cfg-maxconn').value = 300;
    document.getElementById('pg-cfg-sharedbuf').value = '1GB';
    document.getElementById('pg-cfg-effcache').value = '4GB';
    document.getElementById('pg-cfg-workmem').value = '32MB';
    document.getElementById('pg-cfg-maintmem').value = '256MB';
    document.getElementById('pg-cfg-timezone').value = 'Asia/Jakarta';
    document.getElementById('pg-cfg-slowlog').value = 1000;
  }
}

async function submitPostgresConfig(event) {
  event.preventDefault();
  setConfigInlineAlert('pg-cfg-alert', '');

  const version = document.getElementById('pg-cfg-version').value;
  const port = parseInt(document.getElementById('pg-cfg-port').value.trim(), 10);
  const listenAddresses = document.getElementById('pg-cfg-listen').value.trim();
  const remoteMode = document.getElementById('pg-cfg-remote-mode').value;
  const passwordEncryption = document.getElementById('pg-cfg-auth').value;
  const newPostgresPassword = document.getElementById('pg-cfg-newpass').value;
  const maxConnections = parseInt(document.getElementById('pg-cfg-maxconn').value.trim(), 10);
  const sharedBuffers = document.getElementById('pg-cfg-sharedbuf').value.trim();
  const effectiveCacheSize = document.getElementById('pg-cfg-effcache').value.trim();
  const workMem = document.getElementById('pg-cfg-workmem').value.trim();
  const maintenanceWorkMem = document.getElementById('pg-cfg-maintmem').value.trim();
  const timezone = document.getElementById('pg-cfg-timezone').value.trim();
  const slowLogRaw = document.getElementById('pg-cfg-slowlog').value.trim();
  const logMinDurationStatement = parseInt(slowLogRaw, 10);
  const restart = document.getElementById('pg-cfg-restart').checked;

  if (!port || port < 1 || port > 65535) {
    setConfigInlineAlert('pg-cfg-alert', 'Port harus berada di antara 1 sampai 65535.');
    document.getElementById('pg-cfg-port').focus();
    return;
  }
  if (Number.isNaN(logMinDurationStatement)) {
    setConfigInlineAlert('pg-cfg-alert', 'log_min_duration_statement harus berupa angka (mis. -1 atau 500).');
    document.getElementById('pg-cfg-slowlog').focus();
    return;
  }
  if (remoteMode !== 'local' && listenAddresses === 'localhost') {
    setConfigInlineAlert('pg-cfg-alert', 'Aturan remote diaktifkan: ubah Listen Addresses menjadi * agar koneksi dari luar localhost dapat diterima.', 'warning');
  }

  const btn = document.getElementById('btn-save-pg-cfg');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';
  }
  try {
    const saved = await postServiceConfig('postgresql', {
      version,
      port,
      listen_addresses: listenAddresses,
      remote_access_mode: remoteMode,
      password_encryption: passwordEncryption,
      new_postgres_password: newPostgresPassword,
      max_connections: maxConnections,
      shared_buffers: sharedBuffers,
      effective_cache_size: effectiveCacheSize,
      work_mem: workMem,
      maintenance_work_mem: maintenanceWorkMem,
      timezone,
      log_min_duration_statement: logMinDurationStatement,
      restart
    });
    if (!saved) return;
    closeServiceConfigDialog('modal-config-postgres');
    loadStatus();
    await modalAlert(`Konfigurasi PostgreSQL ${saved.version || ''} berhasil disimpan dan diterapkan.`, 'success', 'Konfigurasi PostgreSQL');
  } catch (err) {
    setConfigInlineAlert('pg-cfg-alert', err.message || 'Gagal menyimpan konfigurasi PostgreSQL.');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = 'Simpan Konfigurasi';
    }
  }
}

// --- PHP & PHP-FPM ---
function populatePHPConfigForm(cfg) {
  setConfigInlineAlert('php-cfg-alert', '');
  const meta = document.getElementById('php-cfg-meta');
  if (meta) {
    meta.textContent = `${cfg.dropin_path || cfg.ini_path || '-'} • Pool: ${cfg.pool_path || '-'} • Unit: ${cfg.service_unit || '-'} (${cfg.active ? 'Aktif' : 'Nonaktif'})`;
  }

  const verSelect = document.getElementById('php-cfg-version');
  if (verSelect) {
    const versions = (cfg.available_versions && cfg.available_versions.length > 0)
      ? cfg.available_versions
      : [cfg.version || '8.4'];
    verSelect.innerHTML = versions.map(v =>
      `<option value="${escapeHtml(v)}" ${v === cfg.version ? 'selected' : ''}>PHP ${escapeHtml(v)}</option>`
    ).join('');
  }

  document.getElementById('php-cfg-timezone').value = cfg.timezone || 'UTC';
  document.getElementById('php-cfg-memlimit').value = cfg.memory_limit || '256M';
  document.getElementById('php-cfg-upload').value = cfg.upload_max_filesize || '64M';
  document.getElementById('php-cfg-postmax').value = cfg.post_max_size || '64M';
  document.getElementById('php-cfg-maxexec').value = (cfg.max_execution_time !== undefined) ? cfg.max_execution_time : 60;
  document.getElementById('php-cfg-maxvars').value = cfg.max_input_vars || 3000;
  document.getElementById('php-cfg-opcache-mem').value = cfg.opcache_memory_consumption || 128;
  document.getElementById('php-cfg-display-errors').checked = Boolean(cfg.display_errors);
  document.getElementById('php-cfg-opcache').checked = Boolean(cfg.opcache_enable);
  document.getElementById('php-cfg-sync-cli').checked = (cfg.sync_cli !== undefined) ? Boolean(cfg.sync_cli) : true;

  document.getElementById('php-cfg-pm').value = cfg.pm || 'dynamic';
  document.getElementById('php-cfg-maxchildren').value = cfg.pm_max_children || 10;
  document.getElementById('php-cfg-startservers').value = cfg.pm_start_servers || 2;
  document.getElementById('php-cfg-minspare').value = cfg.pm_min_spare_servers || 1;
  document.getElementById('php-cfg-maxspare').value = cfg.pm_max_spare_servers || 4;
  document.getElementById('php-cfg-maxreq').value = (cfg.pm_max_requests !== undefined) ? cfg.pm_max_requests : 500;
  document.getElementById('php-cfg-restart').checked = true;

  togglePHPPoolDynamicFields();
}

function togglePHPPoolDynamicFields() {
  const pm = document.getElementById('php-cfg-pm')?.value || 'dynamic';
  const isDynamic = (pm === 'dynamic');
  document.querySelectorAll('.php-dynamic-only').forEach((group) => {
    group.style.display = isDynamic ? '' : 'none';
    group.querySelectorAll('input, select').forEach((el) => {
      el.disabled = !isDynamic;
    });
  });
}

async function onPHPConfigVersionChange() {
  const ver = document.getElementById('php-cfg-version')?.value || '';
  try {
    const cfg = await fetchServiceConfig('php-fpm', ver);
    if (cfg) populatePHPConfigForm(cfg);
  } catch (err) {
    setConfigInlineAlert('php-cfg-alert', `Gagal memuat PHP ${ver}: ${err.message}`);
  }
}

function applyPHPPreset(preset) {
  setConfigInlineAlert('php-cfg-alert', '');
  if (preset === 'dev') {
    document.getElementById('php-cfg-timezone').value = 'Asia/Jakarta';
    document.getElementById('php-cfg-memlimit').value = '512M';
    document.getElementById('php-cfg-upload').value = '128M';
    document.getElementById('php-cfg-postmax').value = '128M';
    document.getElementById('php-cfg-maxexec').value = 300;
    document.getElementById('php-cfg-maxvars').value = 5000;
    document.getElementById('php-cfg-display-errors').checked = true;
    document.getElementById('php-cfg-opcache').checked = true;
    document.getElementById('php-cfg-opcache-mem').value = 128;
    document.getElementById('php-cfg-pm').value = 'ondemand';
    document.getElementById('php-cfg-maxchildren').value = 10;
    document.getElementById('php-cfg-maxreq').value = 500;
  } else if (preset === 'prod') {
    document.getElementById('php-cfg-timezone').value = 'Asia/Jakarta';
    document.getElementById('php-cfg-memlimit').value = '512M';
    document.getElementById('php-cfg-upload').value = '64M';
    document.getElementById('php-cfg-postmax').value = '64M';
    document.getElementById('php-cfg-maxexec').value = 120;
    document.getElementById('php-cfg-maxvars').value = 3000;
    document.getElementById('php-cfg-display-errors').checked = false;
    document.getElementById('php-cfg-opcache').checked = true;
    document.getElementById('php-cfg-opcache-mem').value = 256;
    document.getElementById('php-cfg-pm').value = 'dynamic';
    document.getElementById('php-cfg-maxchildren').value = 20;
    document.getElementById('php-cfg-startservers').value = 4;
    document.getElementById('php-cfg-minspare').value = 2;
    document.getElementById('php-cfg-maxspare').value = 6;
    document.getElementById('php-cfg-maxreq').value = 500;
  }
  togglePHPPoolDynamicFields();
}

async function submitPHPConfig(event) {
  event.preventDefault();
  setConfigInlineAlert('php-cfg-alert', '');

  const pm = document.getElementById('php-cfg-pm').value;
  const maxChildren = parseInt(document.getElementById('php-cfg-maxchildren').value.trim(), 10);
  const startServers = parseInt(document.getElementById('php-cfg-startservers').value.trim(), 10) || 2;
  const minSpare = parseInt(document.getElementById('php-cfg-minspare').value.trim(), 10) || 1;
  const maxSpare = parseInt(document.getElementById('php-cfg-maxspare').value.trim(), 10) || 3;

  if (pm === 'dynamic') {
    if (minSpare > maxSpare || maxSpare > maxChildren || startServers < minSpare || startServers > maxSpare) {
      setConfigInlineAlert('php-cfg-alert', `Untuk mode dynamic: pastikan min_spare (${minSpare}) <= start_servers (${startServers}) <= max_spare (${maxSpare}) <= max_children (${maxChildren}).`);
      document.getElementById('php-cfg-startservers').focus();
      return;
    }
  }

  const btn = document.getElementById('btn-save-php-cfg');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';
  }
  try {
    const saved = await postServiceConfig('php-fpm', {
      version: document.getElementById('php-cfg-version').value,
      timezone: document.getElementById('php-cfg-timezone').value.trim(),
      memory_limit: document.getElementById('php-cfg-memlimit').value.trim(),
      upload_max_filesize: document.getElementById('php-cfg-upload').value.trim(),
      post_max_size: document.getElementById('php-cfg-postmax').value.trim(),
      max_execution_time: parseInt(document.getElementById('php-cfg-maxexec').value.trim(), 10),
      max_input_vars: parseInt(document.getElementById('php-cfg-maxvars').value.trim(), 10),
      opcache_memory_consumption: parseInt(document.getElementById('php-cfg-opcache-mem').value.trim(), 10),
      display_errors: document.getElementById('php-cfg-display-errors').checked,
      opcache_enable: document.getElementById('php-cfg-opcache').checked,
      sync_cli: document.getElementById('php-cfg-sync-cli').checked,
      pm,
      pm_max_children: maxChildren,
      pm_start_servers: startServers,
      pm_min_spare_servers: minSpare,
      pm_max_spare_servers: maxSpare,
      pm_max_requests: parseInt(document.getElementById('php-cfg-maxreq').value.trim(), 10) || 0,
      restart: document.getElementById('php-cfg-restart').checked
    });
    if (!saved) return;
    closeServiceConfigDialog('modal-config-php');
    loadStatus();
    await modalAlert(`Konfigurasi PHP ${saved.version || ''} & PHP-FPM berhasil disimpan dan divalidasi.`, 'success', 'Konfigurasi PHP');
  } catch (err) {
    setConfigInlineAlert('php-cfg-alert', err.message || 'Gagal menyimpan konfigurasi PHP.');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = 'Simpan Konfigurasi';
    }
  }
}

// --- Nginx Global ---
function populateNginxConfigForm(cfg) {
  setConfigInlineAlert('nginx-cfg-alert', '');
  const meta = document.getElementById('nginx-cfg-meta');
  if (meta) {
    meta.textContent = `${cfg.config_path || '-'} • Unit: ${cfg.service_unit || 'nginx'} (${cfg.active ? 'Aktif' : 'Nonaktif'})`;
  }
  document.getElementById('nginx-cfg-workers').value = cfg.worker_processes || 'auto';
  document.getElementById('nginx-cfg-conn').value = cfg.worker_connections || 1024;
  document.getElementById('nginx-cfg-bodysize').value = cfg.client_max_body_size || '64M';
  document.getElementById('nginx-cfg-keepalive').value = (cfg.keepalive_timeout !== undefined) ? cfg.keepalive_timeout : 65;
  document.getElementById('nginx-cfg-gzip').checked = Boolean(cfg.gzip);
  document.getElementById('nginx-cfg-tokens').checked = Boolean(cfg.server_tokens);
  document.getElementById('nginx-cfg-restart').checked = true;
}

function applyNginxPreset(preset) {
  setConfigInlineAlert('nginx-cfg-alert', '');
  if (preset === 'dev') {
    document.getElementById('nginx-cfg-workers').value = 'auto';
    document.getElementById('nginx-cfg-conn').value = 1024;
    document.getElementById('nginx-cfg-bodysize').value = '64M';
    document.getElementById('nginx-cfg-keepalive').value = 65;
    document.getElementById('nginx-cfg-gzip').checked = true;
    document.getElementById('nginx-cfg-tokens').checked = true;
  } else if (preset === 'upload') {
    document.getElementById('nginx-cfg-workers').value = 'auto';
    document.getElementById('nginx-cfg-conn').value = 2048;
    document.getElementById('nginx-cfg-bodysize').value = '256M';
    document.getElementById('nginx-cfg-keepalive').value = 120;
    document.getElementById('nginx-cfg-gzip').checked = true;
    document.getElementById('nginx-cfg-tokens').checked = false;
  } else if (preset === 'prod') {
    document.getElementById('nginx-cfg-workers').value = 'auto';
    document.getElementById('nginx-cfg-conn').value = 4096;
    document.getElementById('nginx-cfg-bodysize').value = '64M';
    document.getElementById('nginx-cfg-keepalive').value = 65;
    document.getElementById('nginx-cfg-gzip').checked = true;
    document.getElementById('nginx-cfg-tokens').checked = false;
  }
}

async function submitNginxConfig(event) {
  event.preventDefault();
  setConfigInlineAlert('nginx-cfg-alert', '');

  const btn = document.getElementById('btn-save-nginx-cfg');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Menyimpan...';
  }
  try {
    const saved = await postServiceConfig('nginx', {
      worker_processes: document.getElementById('nginx-cfg-workers').value.trim(),
      worker_connections: parseInt(document.getElementById('nginx-cfg-conn').value.trim(), 10),
      client_max_body_size: document.getElementById('nginx-cfg-bodysize').value.trim(),
      keepalive_timeout: parseInt(document.getElementById('nginx-cfg-keepalive').value.trim(), 10),
      gzip: document.getElementById('nginx-cfg-gzip').checked,
      server_tokens: document.getElementById('nginx-cfg-tokens').checked,
      restart: document.getElementById('nginx-cfg-restart').checked
    });
    if (!saved) return;
    closeServiceConfigDialog('modal-config-nginx');
    loadStatus();
    await modalAlert('Konfigurasi global Nginx berhasil disimpan dan divalidasi.', 'success', 'Konfigurasi Nginx');
  } catch (err) {
    setConfigInlineAlert('nginx-cfg-alert', err.message || 'Gagal menyimpan konfigurasi Nginx.');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = 'Simpan Konfigurasi';
    }
  }
}

// --- Redis Inspector & Flush ---
async function loadRedisInspect() {
  setConfigInlineAlert('redis-inspect-alert', '');
  const statsEl = document.getElementById('redis-inspect-stats');
  const tbody = document.getElementById('redis-inspect-keyspace-body');
  if (statsEl) statsEl.innerHTML = '<div style="color:var(--text-muted);font-size:12.5px;">Mengambil statistik Redis...</div>';
  if (tbody) tbody.innerHTML = '';

  try {
    let resData;
    while (true) {
      const res = await fetch('/api/service-config/redis/inspect');
      const data = await res.json().catch(() => ({}));
      if (data.status === 'sudo_required') {
        const ok = await promptAndVerifySudo(data.message);
        if (!ok) return;
        continue;
      }
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      resData = data.inspect;
      break;
    }

    if (statsEl && resData) {
      const hitRate = (resData.hit_rate_percent || 0).toFixed(1);
      const uptimeMin = Math.floor((resData.uptime_seconds || 0) / 60);
      statsEl.innerHTML = `
        <div class="inspect-stat-card">
          <div class="inspect-stat-label">Versi / Mode</div>
          <div class="inspect-stat-value">${escapeHtml(resData.version || '-')} (${escapeHtml(resData.mode || 'standalone')})</div>
        </div>
        <div class="inspect-stat-card">
          <div class="inspect-stat-label">Memori Terpakai</div>
          <div class="inspect-stat-value">${escapeHtml(resData.used_memory_human || '-')}</div>
        </div>
        <div class="inspect-stat-card">
          <div class="inspect-stat-label">Peak / Max Memory</div>
          <div class="inspect-stat-value">${escapeHtml(resData.peak_memory_human || '-')} / ${escapeHtml(resData.maxmemory_human || 'Unlimited')}</div>
        </div>
        <div class="inspect-stat-card">
          <div class="inspect-stat-label">Klien / Uptime</div>
          <div class="inspect-stat-value">${resData.connected_clients || 0} klien • ${uptimeMin}m</div>
        </div>
        <div class="inspect-stat-card">
          <div class="inspect-stat-label">Cache Hit Rate</div>
          <div class="inspect-stat-value">${hitRate}% (${resData.keyspace_hits || 0} / ${resData.keyspace_misses || 0})</div>
        </div>
        <div class="inspect-stat-card">
          <div class="inspect-stat-label">Total Perintah</div>
          <div class="inspect-stat-value">${resData.total_commands_processed || 0}</div>
        </div>
      `;
    }

    if (tbody) {
      const ks = (resData && resData.keyspaces) || [];
      if (ks.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:14px;">Belum ada key tersimpan di database mana pun.</td></tr>';
      } else {
        tbody.innerHTML = ks.map(item => `
          <tr>
            <td><strong>${escapeHtml(item.db)}</strong></td>
            <td>${item.keys}</td>
            <td>${item.expires}</td>
            <td>${item.avg_ttl} ms</td>
            <td><button type="button" class="btn btn-danger btn-sm" onclick="flushRedisDB(${item.index})">Flush ${escapeHtml(item.db)}</button></td>
          </tr>
        `).join('');
      }
    }
  } catch (err) {
    if (statsEl) statsEl.innerHTML = '';
    setConfigInlineAlert('redis-inspect-alert', err.message || 'Gagal menginspeksi Redis.');
  }
}

async function flushRedisDB(dbIndex) {
  const confirmed = await modalConfirm(`Kosongkan seluruh key pada db${dbIndex}? Tindakan ini tidak dapat dibatalkan.`, {
    title: `Flush Redis db${dbIndex}`,
    confirmText: 'Ya, Flush DB',
    type: 'danger'
  });
  if (!confirmed) return;

  try {
    const bodyPayload = { db: dbIndex };
    while (true) {
      const res = await fetch('/api/service-config/redis/flush', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(bodyPayload)
      });
      const data = await res.json().catch(() => ({}));
      if (data.status === 'sudo_required' || res.status === 401) {
        const pass = await modalPrompt('Operasi flush Redis memerlukan password sudo:', {
          title: 'Akses Sudo Diperlukan',
          isPassword: true
        });
        if (!pass) return;
        bodyPayload.sudo_pass = pass;
        continue;
      }
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      break;
    }
    await loadRedisInspect();
  } catch (err) {
    setConfigInlineAlert('redis-inspect-alert', err.message || 'Gagal melakukan flush DB.');
  }
}

// --- PostgreSQL Database, Role & Connections Inspector ---
async function loadPostgresInspect() {
  setConfigInlineAlert('pg-inspect-alert', '');
  const ver = document.getElementById('pg-cfg-version')?.value || '';
  const dbBody = document.getElementById('pg-inspect-db-body');
  const rolesEl = document.getElementById('pg-inspect-roles');
  const connBody = document.getElementById('pg-inspect-conn-body');
  if (dbBody) dbBody.innerHTML = '<tr><td colspan="3" style="text-align:center;color:var(--text-muted);padding:12px;">Memuat database...</td></tr>';

  try {
    let info;
    const qs = ver ? `?version=${encodeURIComponent(ver)}` : '';
    while (true) {
      const res = await fetch(`/api/service-config/postgresql/inspect${qs}`);
      const data = await res.json().catch(() => ({}));
      if (data.status === 'sudo_required') {
        const ok = await promptAndVerifySudo(data.message);
        if (!ok) return;
        continue;
      }
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      info = data.inspect;
      break;
    }

    if (dbBody) {
      const dbs = (info && info.databases) || [];
      dbBody.innerHTML = dbs.length
        ? dbs.map(d => `<tr><td><strong>${escapeHtml(d.name)}</strong></td><td>${escapeHtml(d.owner)}</td><td>${escapeHtml(d.size)}</td></tr>`).join('')
        : '<tr><td colspan="3" style="text-align:center;color:var(--text-muted);padding:12px;">Tidak ada database ditemukan.</td></tr>';
    }
    if (rolesEl) {
      const roles = (info && info.roles) || [];
      rolesEl.innerHTML = roles.length
        ? roles.map(r => `<span class="stat-badge">${escapeHtml(r.name)}${r.superuser ? ' (superuser)' : ''}${!r.can_login ? ' [nologin]' : ''}</span>`).join('')
        : '<span style="color:var(--text-muted);font-size:12px;">Tidak ada role terdeteksi.</span>';
    }
    if (connBody) {
      const conns = (info && info.connections) || [];
      connBody.innerHTML = conns.length
        ? conns.map(c => `<tr><td>${c.pid}</td><td>${escapeHtml(c.database || '-')}</td><td>${escapeHtml(c.user || '-')}</td><td>${escapeHtml(c.client_addr || 'local')}</td><td>${escapeHtml(c.state || '-')}</td></tr>`).join('')
        : '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:12px;">Tidak ada koneksi aktif selain sesi inspeksi.</td></tr>';
    }
  } catch (err) {
    if (dbBody) dbBody.innerHTML = '';
    setConfigInlineAlert('pg-inspect-alert', err.message || 'Gagal memuat data PostgreSQL.');
  }
}

async function submitCreatePostgresDB(event) {
  event.preventDefault();
  setConfigInlineAlert('pg-inspect-alert', '');

  const version = document.getElementById('pg-cfg-version')?.value || '';
  const database = document.getElementById('pg-new-db-name').value.trim();
  const owner = document.getElementById('pg-new-db-owner').value.trim();
  const password = document.getElementById('pg-new-db-pass').value;

  const btn = document.getElementById('btn-pg-create-db');
  if (btn) {
    btn.disabled = true;
    btn.textContent = 'Membuat...';
  }
  try {
    const bodyPayload = { version, database, owner, password };
    while (true) {
      const res = await fetch('/api/service-config/postgresql/create-db', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(bodyPayload)
      });
      const data = await res.json().catch(() => ({}));
      if (data.status === 'sudo_required' || res.status === 401) {
        const pass = await modalPrompt('Membuat database PostgreSQL memerlukan password sudo:', {
          title: 'Akses Sudo Diperlukan',
          isPassword: true
        });
        if (!pass) return;
        bodyPayload.sudo_pass = pass;
        continue;
      }
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      break;
    }
    document.getElementById('pg-new-db-name').value = '';
    document.getElementById('pg-new-db-pass').value = '';
    await loadPostgresInspect();
    setConfigInlineAlert('pg-inspect-alert', `Database "${database}" berhasil dibuat.`, 'success');
  } catch (err) {
    setConfigInlineAlert('pg-inspect-alert', err.message || 'Gagal membuat database.');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = 'Buat Database';
    }
  }
}

// --- Service Log Viewer ---
async function loadServiceConfigLogs(serviceName) {
  const map = {
    'redis': { title: 'redis-logs-title', out: 'redis-logs-output', verEl: null },
    'postgresql': { title: 'pg-logs-title', out: 'pg-logs-output', verEl: 'pg-cfg-version' },
    'php-fpm': { title: 'php-logs-title', out: 'php-logs-output', verEl: 'php-cfg-version' },
    'nginx': { title: 'nginx-logs-title', out: 'nginx-logs-output', verEl: null }
  };
  const target = map[serviceName];
  if (!target) return;

  const outEl = document.getElementById(target.out);
  const titleEl = document.getElementById(target.title);
  if (outEl) outEl.textContent = 'Memuat log layanan...';

  const ver = target.verEl ? (document.getElementById(target.verEl)?.value || '') : '';
  const qs = ver ? `?version=${encodeURIComponent(ver)}&lines=80` : '?lines=80';

  try {
    while (true) {
      const res = await fetch(`/api/service-config/${encodeURIComponent(serviceName)}/logs${qs}`);
      const data = await res.json().catch(() => ({}));
      if (data.status === 'sudo_required') {
        const ok = await promptAndVerifySudo(data.message);
        if (!ok) return;
        continue;
      }
      if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
      if (titleEl && data.unit) titleEl.textContent = `Log Layanan (${data.unit})`;
      if (outEl) {
        outEl.textContent = data.logs || '(Log kosong)';
        outEl.scrollTop = outEl.scrollHeight;
      }
      break;
    }
  } catch (err) {
    if (outEl) outEl.textContent = `Gagal memuat log: ${err.message}`;
  }
}
