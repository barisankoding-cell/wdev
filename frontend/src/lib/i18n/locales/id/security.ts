import type { WidenStrings } from '../../types.js';
import type { securityEn } from '../en/security.js';

export const securityId: WidenStrings<typeof securityEn> = {
	title: 'Keamanan & Serangan Siber',
	subtitle: 'Pemantauan serangan siber realtime, deteksi intrusi, dan kontrol firewall IP.',
	refresh: 'Perbarui Data',
	refreshing: 'Memperbarui...',
	autoRefresh: 'Auto Refresh (10d)',
	clearLogs: 'Bersihkan Log',
	clearingLogs: 'Membersihkan...',
	blockIPBtn: 'Blokir Alamat IP',

	// Stats Cards
	attacksToday: 'Serangan Hari Ini',
	attacksTodaySub: 'Percobaan intrusi terdeteksi',
	blockedIPs: 'IP Terblokir',
	blockedIPsSub: 'Sedang diban dari server',
	criticalThreats: 'Ancaman Kritis',
	criticalThreatsSub: 'Tingkat bahaya langsung hari ini',
	highThreats: 'Ancaman Tinggi',
	highThreatsSub: 'Aktivitas mencurigakan berat',

	// Tabs
	tabEvents: 'Log Aktivitas Serangan',
	tabBlocked: 'Daftar IP Terblokir',
	tabAnalytics: 'Analisis Ancaman',

	// Filter & Search
	searchPlaceholder: 'Cari berdasarkan IP, path target, atau payload...',
	filterAllTypes: 'Semua Jenis Serangan',
	filterAllSeverities: 'Semua Tingkat Bahaya',
	liveFeedActive: 'Live Feed Terhubung',
	liveFeedWaiting: 'Menunggu aktivitas baru...',

	// Table Headers
	colTime: 'Waktu',
	colIP: 'IP Asal',
	colCountry: 'Lokasi/Negara',
	colType: 'Jenis Serangan',
	colSeverity: 'Tingkat Bahaya',
	colTarget: 'Endpoint Target',
	colAction: 'Mitigasi',
	colDetails: 'Detail Serangan',
	colActions: 'Aksi',

	// Blocked Table
	colBlockedAt: 'Waktu Blokir',
	colExpiresAt: 'Berlaku Hingga',
	colReason: 'Alasan Pemblokiran',
	permanent: 'Permanen',
	unblockBtn: 'Buka Blokir',
	unblocking: 'Membuka...',

	// Modals & Dialogs
	blockModalTitle: 'Blokir Alamat IP',
	blockModalDesc: 'Blokir alamat IP secara manual agar tidak dapat mengakses panel wdev dan port host.',
	ipLabel: 'Alamat IP',
	ipPlaceholder: 'contoh: 198.51.100.25',
	reasonLabel: 'Alasan',
	reasonPlaceholder: 'contoh: Brute force login API atau scanner port',
	durationLabel: 'Durasi Blokir',
	dur1Hour: '1 Jam',
	dur24Hours: '24 Jam',
	dur7Days: '7 Hari',
	durPermanent: 'Permanen',
	confirmBlock: 'Terapkan Blokir',
	blocking: 'Memblokir...',
	cancel: 'Batal',

	confirmClearTitle: 'Bersihkan Semua Log Keamanan?',
	confirmClearDesc: 'Tindakan ini akan menghapus permanen seluruh riwayat serangan dari database SQLite. IP yang sedang diblokir tetap aktif.',
	confirmClearBtn: 'Ya, Bersihkan Log',

	// Types mapping
	typePanelBruteForce: 'Brute Force Login Panel',
	typeSecurityEntrance: 'Penebakan Security Entrance',
	typeHoneypot: 'Jebakan Decoy / Honeypot',
	typeSudoBruteForce: 'Brute Force Eskalasi Sudo',
	typeWebScanner: 'Scanner Celah / Bot Jahat',
	typeSSHBruteForce: 'Brute Force Login SSH',
	typePortScan: 'Pemindaian Port Host',

	// Severities
	sevCritical: 'Kritis',
	sevHigh: 'Tinggi',
	sevMedium: 'Sedang',
	sevLow: 'Rendah',

	// Actions
	actLogged: 'Dicatat',
	actRateLimited: 'Dibatasi (Rate Limit)',
	actBlocked: 'Diblokir',

	// Messages & Toasts
	toastBlockedSuccess: 'Alamat IP berhasil diblokir',
	toastUnblockedSuccess: 'Pemblokiran alamat IP berhasil dicabut',
	toastClearSuccess: 'Riwayat log keamanan berhasil dibersihkan',
	toastError: 'Terjadi kesalahan',
	emptyEvents: 'Belum ada aktivitas serangan terdeteksi. Server Anda saat ini aman.',
	emptyBlocked: 'Tidak ada alamat IP yang sedang diblokir saat ini.'
};
