import type { WidenStrings } from '../../types.js';
import type { commonEn } from '../en/common.js';

export const commonId: WidenStrings<typeof commonEn> = {
	actions: {
		cancel: 'Batal',
		close: 'Tutup',
		continue: 'Lanjutkan',
		confirm: 'Konfirmasi',
		save: 'Simpan',
		saving: 'Menyimpan...',
		delete: 'Hapus',
		deleting: 'Menghapus...',
		edit: 'Ubah',
		create: 'Buat',
		add: 'Tambah',
		apply: 'Terapkan',
		applying: 'Menerapkan...',
		refresh: 'Segarkan',
		refreshing: 'Menyegarkan...',
		loading: 'Memuat...',
		copy: 'Salin',
		copied: 'Tersalin',
		generate: 'Buat Acak',
		reset: 'Reset',
		back: 'Kembali',
		next: 'Berikutnya',
		previous: 'Sebelumnya',
		search: 'Cari',
		clear: 'Bersihkan',
		start: 'Jalankan',
		stop: 'Hentikan',
		restart: 'Restart',
		enable: 'Aktifkan',
		disable: 'Nonaktifkan',
		install: 'Pasang',
		update: 'Perbarui',
		reinstall: 'Pasang Ulang / Sinkron',
		uninstall: 'Uninstall',
		configure: 'Konfigurasi',
		logs: 'Log',
		details: 'Detail',
		open: 'Buka',
		browse: 'Pilih',
		download: 'Unduh',
		upload: 'Unggah'
	},
	status: {
		active: 'Aktif',
		inactive: 'Nonaktif',
		running: 'Berjalan',
		starting: 'Memulai',
		stopped: 'Berhenti',
		failed: 'Gagal',
		installed: 'Terpasang',
		notInstalled: 'Belum Terpasang',
		notActive: 'Belum Aktif',
		updateAvailable: 'Update Tersedia',
		online: 'Online',
		offline: 'Offline',
		enabled: 'Aktif',
		disabled: 'Nonaktif',
		synced: 'Sinkron',
		needsSync: 'Perlu Sinkron',
		ready: 'Siap',
		unknown: 'Tidak Diketahui'
	},
	theme: {
		label: 'Tema Tampilan',
		shortLabel: 'Tema',
		ariaLabel: 'Pilih tema tampilan',
		system: 'Ikuti Sistem',
		light: 'Mode Terang',
		dark: 'Mode Gelap'
	},
	language: {
		label: 'Bahasa',
		ariaLabel: 'Pilih bahasa antarmuka',
		en: 'English',
		id: 'Bahasa Indonesia',
		enShort: 'EN',
		idShort: 'ID'
	},
	pagination: {
		defaultItemLabel: 'data',
		showing: 'Menampilkan {start}–{end} dari {total} {itemLabel}',
		pageShort: 'Hal.',
		firstPage: 'Halaman pertama',
		previousPage: 'Halaman sebelumnya',
		nextPage: 'Halaman berikutnya',
		lastPage: 'Halaman terakhir'
	},
	passwordInput: {
		defaultPlaceholder: 'Minimal 8 karakter...',
		generateLabel: 'Generate',
		generateTitle: 'Buat password acak yang kuat',
		copyTitle: 'Salin password',
		showTitle: 'Tampilkan password',
		hideTitle: 'Sembunyikan password',
		copiedToast: 'Password disalin ke clipboard',
		copyFailedToast: 'Gagal menyalin password ke clipboard'
	},
	clipboard: {
		copySuccess: '{label} disalin ke clipboard.',
		copyFailed: 'Gagal menyalin {label}.',
		genericCopyFailed: 'Gagal menyalin ke clipboard.'
	},
	a11y: {
		closeDialog: 'Tutup',
		toggleSidebar: 'Alihkan Bilah Sisi',
		loading: 'Memuat'
	},
	errors: {
		uploadAborted: 'Unggahan dibatalkan',
		uploadNetwork: 'Gagal terhubung ke server saat mengunggah berkas',
		uploadFailedHttp: 'Upload gagal (HTTP {status})'
	}
};
