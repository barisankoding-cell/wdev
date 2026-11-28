import type { WidenStrings } from '../../types.js';
import type { authEn } from '../en/auth.js';

export const authId: WidenStrings<typeof authEn> = {
	rail: {
		ariaLabel: 'Informasi Control Plane',
		subtitle: 'Server Control Plane',
		headline: 'Satu portal kendali untuk layanan server Anda.',
		description:
			'Masuk menggunakan akun administrator berwenang untuk mengelola layanan dan konfigurasi sistem secara terpusat.',
		footer: 'Akses terbatas hanya untuk administrator yang berwenang.'
	},
	mobileBrandSub: 'Control Plane',
	title: 'Masuk ke Panel',
	subtitle: 'Masukkan kredensial administrator untuk mengakses control plane server.',
	usernameLabel: 'Username',
	usernamePlaceholder: 'Contoh: admin',
	usernameRequired: 'Masukkan username panel Anda.',
	usernameWhitespace: 'Username tidak boleh hanya berisi spasi.',
	passwordLabel: 'Kata Sandi',
	capsLockActive: 'Caps Lock aktif',
	showPasswordAria: 'Tampilkan kata sandi',
	hidePasswordAria: 'Sembunyikan kata sandi',
	passwordVisibilityNote: 'Perhatian: mengaktifkan ini akan menampilkan kata sandi di layar.',
	passwordRequired: 'Masukkan kata sandi panel Anda.',
	submitBtn: 'Masuk ke Panel',
	verifyingBtn: 'Memverifikasi...',
	totpTitle: 'Verifikasi Dua Faktor (2FA)',
	totpSubtitle: 'Masukkan kode 6-digit dari aplikasi Authenticator (atau recovery code 8 karakter) untuk {username}.',
	totpLabel: 'Kode Authenticator / Pemulihan',
	totpPlaceholder: '123456 atau ABCD-EFGH',
	totpRequired: 'Masukkan kode 2FA 6-digit atau recovery code.',
	totpVerifyBtn: 'Verifikasi & Masuk',
	totpBackBtn: 'Ganti akun',
	lockedTitle: 'Akses Dikunci Sementara',
	failedTitle: 'Autentikasi Gagal',
	fallbackAuthError: 'Autentikasi gagal. Periksa kembali username dan kata sandi Anda.',
	recoveryTrigger: 'Lupa kredensial atau butuh pemulihan akses?',
	recoveryNote:
		'Untuk alasan keamanan, pemulihan atau pengaturan ulang kredensial hanya dapat dilakukan langsung oleh administrator melalui terminal SSH server dengan hak akses root atau sudo.',
	store: {
		sessionExpiredTitle: 'Sesi Berakhir',
		sessionExpiredDesc: 'Sesi login Anda telah berakhir. Silakan login kembali.',
		ipBlockedTitle: 'Akses via IP Diblokir',
		ipBlockedDesc: 'Panel telah diikat ke domain resmi HTTPS. Silakan akses memakai URL domain panel.',
		loginSuccessTitle: 'Login berhasil',
		loginSuccessDesc: 'Selamat datang kembali, {username}.',
		recoveryUsedTitle: 'Recovery Code Sekali Pakai Digunakan',
		recoveryUsedDesc: 'Anda masuk menggunakan recovery code. Tersisa {remaining} kode pemulihan.',
		networkError: 'Tidak dapat terhubung ke server wdev. Periksa koneksi jaringan atau status layanan.',
		logoutTitle: 'Sesi Ditutup',
		logoutDesc: 'Anda telah berhasil logout dari panel.'
	}
};
