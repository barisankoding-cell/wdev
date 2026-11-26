export const securityEn = {
	title: 'Security & Cyber Attacks',
	subtitle: 'Real-time cyber attack detection, intrusion monitoring, and IP firewall control.',
	refresh: 'Refresh Data',
	refreshing: 'Refreshing...',
	autoRefresh: 'Auto Refresh (10s)',
	clearLogs: 'Clear Logs',
	clearingLogs: 'Clearing...',
	blockIPBtn: 'Block IP Address',

	// Stats Cards
	attacksToday: 'Attacks Today',
	attacksTodaySub: 'Detected intrusion attempts',
	blockedIPs: 'Blocked IPs',
	blockedIPsSub: 'Currently banned from server',
	criticalThreats: 'Critical Threats',
	criticalThreatsSub: 'Immediate threat level today',
	highThreats: 'High Threats',
	highThreatsSub: 'Severe suspicious activity',

	// Tabs
	tabEvents: 'Attack Activity Log',
	tabBlocked: 'Blocked IP Addresses',
	tabAnalytics: 'Threat Analytics',

	// Filter & Search
	searchPlaceholder: 'Search by IP, target path, or payload...',
	filterAllTypes: 'All Attack Types',
	filterAllSeverities: 'All Severity Levels',
	liveFeedActive: 'Live Feed Connected',
	liveFeedWaiting: 'Waiting for live events...',

	// Table Headers
	colTime: 'Time',
	colIP: 'Source IP',
	colCountry: 'Origin',
	colType: 'Attack Type',
	colSeverity: 'Severity',
	colTarget: 'Target Endpoint',
	colAction: 'Mitigation',
	colDetails: 'Attack Details',
	colActions: 'Actions',

	// Blocked Table
	colBlockedAt: 'Blocked At',
	colExpiresAt: 'Expires At',
	colReason: 'Block Reason',
	permanent: 'Permanent',
	unblockBtn: 'Unblock',
	unblocking: 'Unblocking...',

	// Modals & Dialogs
	blockModalTitle: 'Block IP Address',
	blockModalDesc: 'Manually block an IP address from accessing wdev panel and host ports.',
	ipLabel: 'IP Address',
	ipPlaceholder: 'e.g. 198.51.100.25',
	reasonLabel: 'Reason',
	reasonPlaceholder: 'e.g. API Brute force or port scanner',
	durationLabel: 'Block Duration',
	dur1Hour: '1 Hour',
	dur24Hours: '24 Hours',
	dur7Days: '7 Days',
	durPermanent: 'Permanent',
	confirmBlock: 'Block IP',
	blocking: 'Blocking...',
	cancel: 'Cancel',

	confirmClearTitle: 'Clear All Security Logs?',
	confirmClearDesc: 'This will permanently remove all past attack records from SQLite. Active blocked IPs will remain banned.',
	confirmClearBtn: 'Yes, Clear All Logs',

	// Types mapping
	typePanelBruteForce: 'Panel Login Brute Force',
	typeSecurityEntrance: 'Security Entrance Probe',
	typeHoneypot: 'Decoy / Honeypot Probe',
	typeSudoBruteForce: 'Sudo Escalation Brute Force',
	typeWebScanner: 'Vulnerability / Bot Scanner',
	typeSSHBruteForce: 'SSH Login Brute Force',
	typePortScan: 'Port Scanner Probe',

	// Severities
	sevCritical: 'Critical',
	sevHigh: 'High',
	sevMedium: 'Medium',
	sevLow: 'Low',

	// Actions
	actLogged: 'Logged',
	actRateLimited: 'Rate Limited',
	actBlocked: 'Blocked',

	// Messages & Toasts
	toastBlockedSuccess: 'IP address blocked successfully',
	toastUnblockedSuccess: 'IP address unblocked successfully',
	toastClearSuccess: 'Security logs cleared successfully',
	toastError: 'An error occurred',
	emptyEvents: 'No cyber attack activity detected yet. Your server is safe.',
	emptyBlocked: 'No IP addresses are currently blocked.'
};
