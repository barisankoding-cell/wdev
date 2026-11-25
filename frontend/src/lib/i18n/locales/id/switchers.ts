import type { WidenStrings } from '../../types.js';
import type { switchersEn } from '../en/switchers.js';

export const switchersId: WidenStrings<typeof switchersEn> = {
	title: 'Pengelola Multi-Versi Runtime',
	subtitle: 'Beralih versi runtime Node.js ({nodeCmd}), PHP CLI & FPM ({phpCmd}), PostgreSQL ({pgCmd}), serta Java OpenJDK dengan satu klik.',
	loadFailed: 'Gagal memuat daftar runtime',
	activeVersion: 'Versi Aktif:',
	configBtn: 'Config',
	switchToLabel: 'Ganti ke Versi'
};
