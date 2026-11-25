import { commonEn } from './common.js';
import { layoutEn } from './layout.js';
import { authEn } from './auth.js';
import { storesEn } from './stores.js';
import { modalsEn } from './modals.js';
import { dashboardEn } from './dashboard.js';
import { switchersEn } from './switchers.js';
import { toolchainsEn } from './toolchains.js';
import { workspaceEn } from './workspace.js';
import { setupEn } from './setup.js';
import { portsEn } from './ports.js';
import { terminalEn } from './terminal.js';
import { webstackEn } from './webstack.js';
import { composeEn } from './compose.js';
import { filesEn } from './files.js';
import { powerdnsEn } from './powerdns.js';
import { cloudflareEn } from './cloudflare.js';
import { mailEn } from './mail.js';

export const enMessages = {
	common: commonEn,
	layout: layoutEn,
	auth: authEn,
	stores: storesEn,
	modals: modalsEn,
	dashboard: dashboardEn,
	switchers: switchersEn,
	toolchains: toolchainsEn,
	workspace: workspaceEn,
	setup: setupEn,
	ports: portsEn,
	terminal: terminalEn,
	webstack: webstackEn,
	compose: composeEn,
	files: filesEn,
	powerdns: powerdnsEn,
	cloudflare: cloudflareEn,
	mail: mailEn
};

export type TranslationSchema = typeof enMessages;
