import type { WidenStrings } from '../../types.js';
import type { TranslationSchema } from '../en/index.js';
import { commonId } from './common.js';
import { layoutId } from './layout.js';
import { authId } from './auth.js';
import { storesId } from './stores.js';
import { modalsId } from './modals.js';
import { dashboardId } from './dashboard.js';
import { switchersId } from './switchers.js';
import { toolchainsId } from './toolchains.js';
import { workspaceId } from './workspace.js';
import { setupId } from './setup.js';
import { portsId } from './ports.js';
import { terminalId } from './terminal.js';
import { webstackId } from './webstack.js';
import { composeId } from './compose.js';
import { filesId } from './files.js';
import { powerdnsId } from './powerdns.js';
import { cloudflareId } from './cloudflare.js';
import { mailId } from './mail.js';

export const idMessages: WidenStrings<TranslationSchema> = {
	common: commonId,
	layout: layoutId,
	auth: authId,
	stores: storesId,
	modals: modalsId,
	dashboard: dashboardId,
	switchers: switchersId,
	toolchains: toolchainsId,
	workspace: workspaceId,
	setup: setupId,
	ports: portsId,
	terminal: terminalId,
	webstack: webstackId,
	compose: composeId,
	files: filesId,
	powerdns: powerdnsId,
	cloudflare: cloudflareId,
	mail: mailId
};
