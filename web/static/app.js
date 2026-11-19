// wdev Dashboard Application
document.addEventListener('DOMContentLoaded', () => {
  initModalDialog();
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

  confirmBtn.addEventListener('click', () => {
    dialog.close();
    if (currentModalResolve) {
      if (currentModalMode === 'prompt') {
        currentModalResolve(promptInput.value.trim());
      } else if (currentModalMode === 'confirm') {
        currentModalResolve(true);
      } else {
        currentModalResolve(true);
      }
      currentModalResolve = null;
    }
  });

  cancelBtn.addEventListener('click', () => {
    dialog.close();
    if (currentModalResolve) {
      if (currentModalMode === 'prompt') {
        currentModalResolve(null);
      } else {
        currentModalResolve(false);
      }
      currentModalResolve = null;
    }
  });

  // Handle native Cancel / Escape
  dialog.addEventListener('cancel', (e) => {
    e.preventDefault();
    dialog.close();
    if (currentModalResolve) {
      if (currentModalMode === 'prompt') {
        currentModalResolve(null);
      } else if (currentModalMode === 'confirm') {
        currentModalResolve(false);
      } else {
        currentModalResolve(true);
      }
      currentModalResolve = null;
    }
  });

  // Light dismiss: clicking outside the dialog card closes it
  dialog.addEventListener('click', (e) => {
    const card = dialog.querySelector('.system-dialog-card');
    if (!card) return;
    const r = card.getBoundingClientRect();
    const clickedOutside = (
      e.clientX < r.left || e.clientX > r.right ||
      e.clientY < r.top || e.clientY > r.bottom
    );
    if (clickedOutside) {
      dialog.close();
      if (currentModalResolve) {
        if (currentModalMode === 'prompt') {
          currentModalResolve(null);
        } else if (currentModalMode === 'confirm') {
          currentModalResolve(false);
        } else {
          currentModalResolve(true);
        }
        currentModalResolve = null;
      }
    }
  });

  // Allow Enter key in prompt input
  promptInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      confirmBtn.click();
    }
  });
}

function modalAlert(message, type = 'info', title = '') {
  return new Promise((resolve) => {
    currentModalMode = 'alert';
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
  return new Promise((resolve) => {
    currentModalMode = 'confirm';
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
  return new Promise((resolve) => {
    currentModalMode = 'prompt';
    currentModalResolve = resolve;

    const {
      title = 'Input Data',
      placeholder = '',
      defaultValue = '',
      confirmText = 'Simpan',
      cancelText = 'Batal',
      isPassword = false
    } = (typeof options === 'string') ? { title: options } : options;

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
        'webstack': 'Web Stack & Domain Lokal (dev-site)',
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
      actionsHtml = `
        <button class="btn btn-sm btn-warning" onclick="triggerInstall('${s.install_component}')" title="Perbarui ke versi ${escapeHtml(s.latest_version)}" style="display:inline-flex;align-items:center;gap:6px;flex:1;">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
          <span>Perbarui ke ${escapeHtml(s.latest_version)}</span>
        </button>
        ${controlBtn}
      `;
    } else {
      // Installed & up-to-date
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
            <button class="btn btn-sm btn-danger" onclick="serviceAction('${s.name}', 'stop')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="6" y="6" width="12" height="12" rx="2"/></svg> Hentikan</button>
          `;
        } else {
          actionsHtml = `
            <button class="btn btn-sm btn-primary" onclick="serviceAction('${s.name}', 'start')" style="display:inline-flex;align-items:center;gap:4px;"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"/></svg> Jalankan</button>
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

      card.innerHTML = `
        <div>
          <div class="service-name">${item.name}</div>
          <div class="service-meta">Versi Aktif: <strong>${item.active_version}</strong></div>
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

async function switchRuntime(type, ver, sudoPass = null) {
  const labelMap = {
    'node': 'Node.js',
    'php': 'PHP',
    'pg': 'PostgreSQL',
    'java': 'Java OpenJDK'
  };
  const label = labelMap[type] || type.toUpperCase();

  if (!sudoPass) {
    const confirmed = await modalConfirm(`Beralih versi runtime ${label} ke versi ${ver}?`, {
      title: 'Konfirmasi Pengalihan Runtime',
      confirmText: 'Beralih Sekarang',
      type: 'primary'
    });
    if (!confirmed) return;
  }

  try {
    const payload = { version: ver };
    if (sudoPass) payload.sudo_pass = sudoPass;

    const res = await fetch(`/api/switchers/${type}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();

    if (data.status === 'sudo_required') {
      const pass = await modalPrompt(
        `Pengalihan runtime ${label} memerlukan hak akses administrator (sudo) sistem.\nSilakan masukkan password sudo Anda:`,
        {
          title: 'Akses Sudo Diperlukan',
          placeholder: 'Masukkan password sudo...',
          confirmText: 'Lanjutkan',
          cancelText: 'Batal',
          isPassword: true
        }
      );
      if (pass) {
        await switchRuntime(type, ver, pass);
      }
      return;
    }

    if (!res.ok) {
      await modalAlert(`Gagal beralih runtime: ${data.error || 'Terjadi kesalahan'}`, 'error', 'Ganti Runtime');
    } else {
      // Buka live terminal modal agar pengguna melihat prosesnya secara real-time
      openTerminalModal(`Switch ${label} -> ${ver}`);
    }
  } catch (err) {
    await modalAlert('Error koneksi: ' + err, 'error', 'Ganti Runtime');
  }
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

async function submitAddZone() {
  const zone = await modalPrompt('Masukkan nama zone domain authoritative (contoh: domainanda.com):', {
    title: 'Tambah DNS Zone',
    placeholder: 'domainanda.com'
  });
  if (!zone) return;

  try {
    const res = await fetch('/api/pdns/zones', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ zone_name: zone })
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
  'orca-desktop': 'Orca ADE Desktop GUI',
  'orca-headless': 'Orca ADE Headless Server'
};

async function triggerInstall(component, sudoPass = null) {
  const friendlyName = COMPONENT_TITLES[component] || component;
  if (!sudoPass) {
    const confirmed = await modalConfirm(`Jalankan operasi instalasi / pembaruan untuk: ${friendlyName}?`, {
      title: 'Konfirmasi Instalasi / Pembaruan',
      confirmText: 'Lanjutkan',
      type: 'primary'
    });
    if (!confirmed) return;
  }

  try {
    const payload = { component: component };
    if (sudoPass) payload.sudo_pass = sudoPass;

    const res = await fetch('/api/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();

    if (data.status === 'sudo_required') {
      const pass = await modalPrompt(
        'Operasi pemeliharaan/instalasi ini memerlukan hak akses administrator (sudo) sistem.\nSilakan masukkan password sudo Anda:',
        {
          title: 'Akses Sudo Diperlukan',
          placeholder: 'Masukkan password sudo...',
          confirmText: 'Lanjutkan',
          cancelText: 'Batal',
          isPassword: true
        }
      );
      if (pass) {
        await triggerInstall(component, pass);
      }
      return;
    }

    if (!res.ok) {
      await modalAlert(`Gagal: ${data.error}`, 'error', 'Instalasi');
    } else {
      // Immediately open live terminal modal
      openTerminalModal(component);
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Instalasi');
  }
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
        relayDomainSelect.innerHTML = data.domains.map(d => `<option value="${d.domain}">${d.domain}</option>`).join('');
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

        let providerBadge = '<span class="stat-badge" style="background:rgba(107,114,128,0.18);color:var(--text-muted);font-size:11px;">Direct MX</span>';
        let hostPort = '<span style="color:var(--text-dim);font-size:12px;">Port 25 (Tanpa Relay)</span>';
        let userStr = '<span style="color:var(--text-dim);font-size:12px;">-</span>';

        if (relay && relay.host) {
          const prov = (relay.provider || 'custom').toUpperCase();
          const badgeColor = prov === 'RESEND' ? 'rgba(59,130,246,0.18);color:var(--primary);' :
                             prov === 'BREVO' ? 'rgba(16,185,129,0.18);color:var(--success);' :
                             'rgba(245,158,11,0.18);color:var(--warning);';
          providerBadge = `<span class="stat-badge" style="background:${badgeColor}font-size:11px;font-weight:600;">● ${prov}</span>`;
          hostPort = `<span style="font-family:monospace;font-size:12px;">${relay.host}:${relay.port}</span>`;
          userStr = `<span style="font-size:12px;color:var(--text-main);">${relay.username || '-'}</span>`;
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
                <span>Setel Relay</span>
              </button>
              <button class="btn btn-sm btn-outline" style="padding:4px 10px;font-size:11.5px;display:inline-flex;align-items:center;gap:4px;" onclick="resetMailAdmin('${d.domain}')" title="Reset password admin domain ${d.domain}">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 2l-2 2m-1.5 1.5L16 7m-1.5 1.5L13 10m-1.5 1.5L10 13m-1.5 1.5L7 16m-4 5a4 4 0 1 1 5.66-5.66l9.9-9.9a2 2 0 0 1 2.83 0l1.41 1.41a2 2 0 0 1 0 2.83L12.9 19.66A4 4 0 0 1 7 21z"/></svg>
                <span>Reset Admin</span>
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
      : '<span class="stat-badge" style="background:rgba(107,114,128,0.18);color:var(--text-muted);font-size:11px;">Direct MX</span>';

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

// Modal: Add Account
function openMailAddAccountModal(domain) {
  const modal = document.getElementById('modal-mail-add-account');
  const emailInput = document.getElementById('mail-new-email');
  const passInput = document.getElementById('mail-new-password');
  const defaultDom = domain || (cachedMailStatus?.domains?.[0]?.domain || '');
  if (emailInput) {
    emailInput.value = defaultDom ? `@${defaultDom}` : '';
    if (defaultDom) {
      emailInput.setSelectionRange(0, 0);
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
  toggleMailRelayFields();
  if (modal) modal.classList.add('active');
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
      await modalAlert(`Konfigurasi outbound SMTP relay domain ${domain} berhasil disimpan!`, 'success', 'Relay Berhasil Disetel');
      closeMailRelayModal();
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
  const prov = document.getElementById('mail-setup-relay-prov')?.value || 'direct';
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
  const relayProv = document.getElementById('mail-setup-relay-prov')?.value || 'direct';

  if (!domain) {
    await modalAlert('Silakan pilih atau masukkan domain publik yang valid.', 'warning', 'Validasi Input');
    return;
  }

  let relayHost = '';
  let relayPort = 587;
  let relayUser = '';
  let relayPass = '';

  if (relayProv === 'resend') {
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
