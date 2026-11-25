import type { WidenStrings } from '../../types.js';
import type { toolchainsEn } from '../en/toolchains.js';

export const toolchainsId: WidenStrings<typeof toolchainsEn> = {
	title: 'Toolchains, Compilers & SDKs',
	subtitle: 'Status versi terpasang, deteksi rilis resmi upstream, serta audit konsistensi PATH sistem.',
	checkingBtn: 'Memeriksa...',
	checkUpdatesBtn: 'Periksa Pembaruan',
	updateAllBtn: 'Perbarui Toolchain',
	refreshedToast: 'Informasi versi resmi toolchain diperbarui.',
	loadFailed: 'Gagal memuat toolchain',
	installedVersion: 'Versi Terpasang',
	notInstalledValue: 'Belum terpasang',
	detailLabel: 'Detail',
	latestRelease: 'Rilis Resmi Terbaru',
	installNowBtn: 'Pasang Sekarang',
	updateToBtn: 'Perbarui ke {version}',
	envTitle: 'Diagnostik Environment & PATH Toolchain',
	envDesc: 'Sumber tunggal environment di {path} dan validasi symlink kanonik.',
	auditBtn: 'Audit Duplikat',
	fixBtn: 'Perbaiki',
	syncPathBtn: 'Sinkronkan PATH',
	effectivePathOrder: 'Urutan PATH Login Shell Efektif:',
	descriptions: {
		rust: 'Toolchain compiler Rust modern dengan linker super cepat Mold dan compiler cache Sccache.',
		go: 'Compiler dan toolchain bahasa Go resmi terbaru di /usr/local/go.',
		node: 'Runtime JavaScript v8 modern LTS dengan package manager npm di /usr/local/lib/nodejs.',
		java: 'Java Development Kit OpenJDK multi-versi (JDK 17, 21 LTS, dan 25).',
		flutter: 'Framework UI aplikasi multiplatform Android, Web, dan Desktop di /usr/local/flutter.',
		android: 'Command Line Tools, Platform Tools, dan SDK Platform API di /usr/local/android-sdk.',
		redis: 'Server in-memory cache lokal berkecepatan tinggi port 6379.',
		base: 'Build tools esensial, clang, cmake, ninja, curl, git, jq, dan pustaka dev distro.'
	}
};
