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
        'switchers': 'Pengelola Multi-Versi Runtime (Switchers)',
        'webstack': 'Web Stack & Domain Lokal (dev-site)',
        'toolchains': 'Toolchains, Compilers & SDKs',
        'workspace': 'Workspace, ADE & Shell',
        'cloudflare': 'Cloudflare Tunnel & DNS Manager',
        'powerdns': 'PowerDNS & VPS Edge Gateway (Netbird)',
        'full-install': 'Instalasi Lengkap (Full Auto Setup)',
        'modular': 'Instalasi Komponen Modular'
      };
      document.getElementById('topbar-page-title').textContent = titleMap[tabName] || 'Dashboard';
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

function renderServices(services) {
  const container = document.getElementById('services-container');
  if (!container || !services) return;
  container.innerHTML = '';

  services.forEach(s => {
    const card = document.createElement('div');
    card.className = 'service-card';

    let actionsHtml = '';
    if (s.is_remote) {
      if (s.active) {
        actionsHtml = `
          <button class="btn btn-sm btn-primary" onclick="switchToTab('powerdns')">Kelola DNS →</button>
          <button class="btn btn-sm" onclick="serviceAction('${s.name}', 'restart')">Tes Koneksi</button>
        `;
      } else {
        actionsHtml = `
          <button class="btn btn-sm btn-primary" onclick="switchToTab('powerdns')">Cek Konfigurasi</button>
          <button class="btn btn-sm" onclick="serviceAction('${s.name}', 'restart')">Coba Hubungkan</button>
        `;
      }
    } else {
      if (s.active) {
        actionsHtml = `
          <button class="btn btn-sm" onclick="serviceAction('${s.name}', 'restart')">Restart</button>
          <button class="btn btn-sm btn-danger" onclick="serviceAction('${s.name}', 'stop')">Hentikan</button>
        `;
      } else {
        actionsHtml = `
          <button class="btn btn-sm btn-primary" onclick="serviceAction('${s.name}', 'start')">Jalankan</button>
        `;
      }
    }

    card.innerHTML = `
      <div class="service-info">
        <div>
          <div class="service-name">${s.display_name}</div>
          <div class="service-meta">${s.category} ${s.port ? '• ' + s.port : ''}</div>
        </div>
        <span class="status-pill ${s.active ? 'active' : 'stopped'}">
          ${s.active ? (s.is_remote ? '● Online (Edge VPS)' : '● Aktif') : '○ Nonaktif'}
        </span>
      </div>
      <div class="service-actions">
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
    badge.className = 'stat-badge';
    badge.innerHTML = `<strong>${name.toUpperCase()}:</strong> <span>${ver}</span>`;
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
      tbody.innerHTML = `<tr><td colspan="6" style="text-align:center;color:var(--text-dim);padding:24px;">Belum ada virtual host. Klik "+ Tambah Virtual Host" untuk membuatnya.</td></tr>`;
      return;
    }

    sites.forEach(s => {
      const tr = document.createElement('tr');
      const proto = s.ssl_enabled ? 'https://' : 'http://';
      tr.innerHTML = `
        <td><strong><a href="${proto}${s.domain}" target="_blank" style="color:var(--primary);text-decoration:none;">${s.domain}</a></strong></td>
        <td><span class="stat-badge" style="display:inline-block;padding:2px 8px;font-size:11px;">${s.framework.toUpperCase()}</span></td>
        <td>${s.framework === 'proxy' ? `Forward :${s.proxy_port}` : `PHP ${s.php_version}`}</td>
        <td><code style="font-size:11px;color:var(--text-muted);">${s.root_path || '-'}</code></td>
        <td>${s.ssl_enabled ? '<span style="color:var(--success);">✓ SSL</span>' : '<span style="color:var(--text-dim);">HTTP</span>'}</td>
        <td>
          <button class="btn btn-sm btn-danger" onclick="deleteSite('${s.domain}')">Hapus</button>
        </td>
      `;
      tbody.appendChild(tr);
    });
    loadPDNSSites();
  } catch (err) {
    console.error('Failed to load sites:', err);
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
        const isCurrent = item.active_version.includes(v);
        buttonsHtml += `
          <button class="btn btn-sm ${isCurrent ? 'btn-primary' : ''}" onclick="switchRuntime('${key}', '${v}')">
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

async function switchRuntime(type, ver) {
  try {
    const res = await fetch(`/api/switchers/${type}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: ver })
    });
    const data = await res.json();
    if (!res.ok) {
      await modalAlert(`Gagal ganti runtime: ${data.error}`, 'error', 'Ganti Runtime');
    } else {
      await modalAlert(`Runtime ${type.toUpperCase()} dialihkan ke ${ver}`, 'success', 'Ganti Runtime');
    }
    loadSwitchers();
    loadStatus();
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Ganti Runtime');
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
            <td><button class="btn btn-sm btn-danger" onclick="unexposeCF('${exp.local_domain}')">Hapus</button></td>
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

function setPDNSExposeMode(mode) {
  currentPDNSExposeMode = mode;
  const btnSite = document.getElementById('pdns-btn-mode-site');
  const btnPort = document.getElementById('pdns-btn-mode-port');
  const grpSite = document.getElementById('pdns-expose-site-group');
  const grpPort = document.getElementById('pdns-expose-port-group');

  if (mode === 'site') {
    if (btnSite) { btnSite.className = 'btn btn-primary btn-sm'; }
    if (btnPort) { btnPort.className = 'btn btn-outline btn-sm'; }
    if (grpSite) grpSite.style.display = 'block';
    if (grpPort) grpPort.style.display = 'none';
  } else {
    if (btnSite) { btnSite.className = 'btn btn-outline btn-sm'; }
    if (btnPort) { btnPort.className = 'btn btn-primary btn-sm'; }
    if (grpSite) grpSite.style.display = 'none';
    if (grpPort) grpPort.style.display = 'block';
  }
}

async function loadPDNSSites() {
  try {
    const res = await fetch('/api/sites');
    if (!res.ok) return;
    const sites = await res.json() || [];
    const select = document.getElementById('pdns-expose-site-select');
    if (!select) return;

    const current = select.value;
    select.innerHTML = sites.length === 0
      ? '<option value="">(Belum ada vhost lokal dibuat)</option>'
      : sites.map(s => `<option value="${s.domain}">${s.domain} (${s.framework})</option>`).join('');

    if (current && sites.some(s => s.domain === current)) {
      select.value = current;
    }
  } catch (err) {
    console.error('Failed to load sites for PDNS:', err);
  }
}

function onPDNSSiteSelected() {
  const select = document.getElementById('pdns-expose-site-select');
  const pubInput = document.getElementById('pdns-expose-site-pub');
  if (!select || !pubInput) return;
  const selDom = select.value.replace(/\.test$/, '').replace(/\.local$/, '');
  if (selDom && !pubInput.value) {
    pubInput.value = selDom + '.erposoft.my.id';
  }
}

async function loadPDNS() {
  try {
    const res = await fetch('/api/pdns/status');
    if (!res.ok) return;
    const conf = await res.json();

    const badge = document.getElementById('pdns-status-badge');
    if (badge) {
      badge.innerHTML = conf.reachable ?
        '<span class="status-pill active">● Terhubung ke Edge VPS</span>' :
        '<span class="status-pill stopped">○ Edge VPS Terputus / Belum Disetel</span>';
    }

    const nbEl = document.getElementById('pdns-netbird-ip');
    if (nbEl) nbEl.textContent = conf.vps_netbird_ip || '-';

    const pubEl = document.getElementById('pdns-public-ip');
    if (pubEl) pubEl.textContent = conf.vps_public_ip || '-';

    const locEl = document.getElementById('pdns-local-ip');
    if (locEl) locEl.textContent = conf.local_netbird_ip || '-';

    await loadPDNSSites();
    await loadPDNSExposures();
    await loadPDNSZones();
  } catch (err) {
    console.error('Failed to load PDNS:', err);
  }
}

async function loadPDNSExposures() {
  const tbody = document.getElementById('pdns-exposures-tbody');
  if (!tbody) return;

  try {
    const res = await fetch('/api/pdns/exposures');
    if (!res.ok) {
      tbody.innerHTML = `<tr><td colspan="4" style="text-align:center;color:var(--text-dim);padding:16px;">Gagal memuat proxy VPS.</td></tr>`;
      return;
    }
    const proxies = await res.json() || [];
    tbody.innerHTML = '';
    if (proxies.length === 0) {
      tbody.innerHTML = `<tr><td colspan="4" style="text-align:center;color:var(--text-dim);padding:16px;">Belum ada proxy atau aplikasi yang diekspos di VPS Edge.</td></tr>`;
      return;
    }

    proxies.forEach(p => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td>
          <a href="https://${p.domain}" target="_blank" rel="noopener" style="color:var(--primary);font-weight:600;text-decoration:none;">
            ${p.domain} ↗
          </a>
        </td>
        <td>
          <span class="status-pill ${p.enabled ? 'active' : 'stopped'}">
            ${p.enabled ? '● Aktif' : '○ Nonaktif'}
          </span>
        </td>
        <td><code style="font-size:11.5px;color:var(--text-muted);">${p.config_file || '-'}</code></td>
        <td>
          <button class="btn btn-sm btn-danger" onclick="unexposePDNS('${p.domain}')">Hapus Ekspos</button>
        </td>
      `;
      tbody.appendChild(tr);
    });
  } catch (err) {
    console.error('Failed to load PDNS exposures:', err);
    tbody.innerHTML = `<tr><td colspan="4" style="text-align:center;color:var(--text-dim);padding:16px;">Edge VPS tidak dapat dihubungi.</td></tr>`;
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
      await modalAlert(`Domain '${payload.public_domain}' berhasil diekspos ke internet! DNS dan reverse-proxy di VPS telah aktif.`, 'success', 'VPS Edge Gateway');
      loadPDNSExposures();
      loadPDNSZones();
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
      loadPDNSExposures();
      loadPDNSZones();
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
    const zones = await res.json() || [];
    tbody.innerHTML = '';
    if (zones.length === 0) {
      tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-dim);padding:16px;">Belum ada zone DNS.</td></tr>`;
      return;
    }

    zones.forEach(z => {
      const tr = document.createElement('tr');
      tr.innerHTML = `
        <td><strong>${z.name}</strong></td>
        <td>${z.kind}</td>
        <td>${z.serial}</td>
        <td><span class="status-pill active">● Aktif</span></td>
        <td>
          <div style="display:flex;gap:6px;">
            <button class="btn btn-sm btn-outline" onclick="openPDNSRecordsModal('${z.name}')">Record</button>
            <button class="btn btn-sm btn-danger" onclick="deletePDNSZone('${z.name}')">Hapus</button>
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
      (rr.records || []).forEach(rec => {
        const tr = document.createElement('tr');
        tr.innerHTML = `
          <td><strong>${rr.name}</strong></td>
          <td><span class="stat-badge" style="font-size:11px;padding:2px 6px;">${rr.type}</span></td>
          <td>${rr.ttl}</td>
          <td style="max-width:260px;word-break:break-all;font-size:12px;font-family:monospace;">${rec.content}</td>
          <td>
            ${isProtected ? '<span style="font-size:11px;color:var(--text-dim);">Sistem</span>' :
              `<button class="btn btn-sm btn-danger" style="padding:2px 8px;font-size:11px;" onclick="deletePDNSRecord('${zoneName}', '${rr.name}', '${rr.type}')">Hapus</button>`}
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

// SSE Task Logs
let eventSource = null;
function setupSSE() {
  if (eventSource) eventSource.close();
  eventSource = new EventSource('/api/tasks/events');

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      appendTerminalLog(data);
    } catch (e) {}
  };

  eventSource.onerror = () => {
    // Reconnect silently
  };
}

function appendTerminalLog(data) {
  const terminal = document.getElementById('terminal-log-output');
  if (!terminal) return;

  const line = document.createElement('div');
  line.style.marginBottom = '2px';
  if (data.type === 'start') {
    line.style.color = '#38bdf8';
    line.style.fontWeight = 'bold';
    line.textContent = `>>> [${new Date(data.timestamp).toLocaleTimeString()}] ${data.message}`;
  } else if (data.type === 'finish') {
    line.style.color = (data.status === 'success') ? '#10b981' : '#ef4444';
    line.style.fontWeight = 'bold';
    line.textContent = `<<< [${new Date(data.timestamp).toLocaleTimeString()}] ${data.message}`;
  } else {
    line.textContent = data.message;
  }

  terminal.appendChild(line);
  terminal.scrollTop = terminal.scrollHeight;
}

function clearTerminal() {
  const terminal = document.getElementById('terminal-log-output');
  if (terminal) terminal.innerHTML = '';
}

async function triggerInstall(component, sudoPass = null) {
  if (!sudoPass) {
    const confirmed = await modalConfirm(`Jalankan instalasi/update untuk: ${component}?`, {
      title: 'Konfirmasi Pemeliharaan',
      confirmText: 'Jalankan',
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
      // Smoothly scroll to live console
      const term = document.getElementById('terminal-log-output');
      if (term) term.scrollIntoView({ behavior: 'smooth' });
    }
  } catch (err) {
    await modalAlert('Error: ' + err, 'error', 'Instalasi');
  }
}

// Dismiss tooltips on Escape key (WCAG 1.4.13)
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    if (document.activeElement && document.activeElement.hasAttribute('data-tooltip')) {
      document.activeElement.blur();
    }
  }
});
