export const authEn = {
	rail: {
		ariaLabel: 'Control Plane Information',
		subtitle: 'Server Control Plane',
		headline: 'One control portal for your server services.',
		description:
			'Sign in with an authorized administrator account to centrally manage server services and system configuration.',
		footer: 'Restricted access for authorized administrators only.'
	},
	mobileBrandSub: 'Control Plane',
	title: 'Sign In to Panel',
	subtitle: 'Enter administrator credentials to access the server control plane.',
	usernameLabel: 'Username',
	usernamePlaceholder: 'Example: admin',
	usernameRequired: 'Enter your panel username.',
	usernameWhitespace: 'Username cannot contain only spaces.',
	passwordLabel: 'Password',
	capsLockActive: 'Caps Lock is on',
	showPasswordAria: 'Show password',
	hidePasswordAria: 'Hide password',
	passwordVisibilityNote: 'Warning: enabling this will reveal your password on screen.',
	passwordRequired: 'Enter your panel password.',
	submitBtn: 'Sign In to Panel',
	verifyingBtn: 'Verifying...',
	totpTitle: 'Two-Factor Verification',
	totpSubtitle: 'Enter the 6-digit code from your Authenticator app (or an 8-character recovery code) for {username}.',
	totpLabel: 'Authenticator / Recovery Code',
	totpPlaceholder: '123456 or ABCD-EFGH',
	totpRequired: 'Enter your 6-digit 2FA code or recovery code.',
	totpVerifyBtn: 'Verify & Sign In',
	totpBackBtn: 'Use another account',
	lockedTitle: 'Access Temporarily Locked',
	failedTitle: 'Authentication Failed',
	fallbackAuthError: 'Authentication failed. Please check your username and password.',
	recoveryTrigger: 'Forgot credentials or need access recovery?',
	recoveryNote:
		'For security reasons, credential recovery or resets can only be performed directly by an administrator via the server SSH terminal with root or sudo privileges.',
	store: {
		sessionExpiredTitle: 'Session Expired',
		sessionExpiredDesc: 'Your login session has expired. Please sign in again.',
		ipBlockedTitle: 'IP Access Blocked',
		ipBlockedDesc:
			'This panel is bound to an official HTTPS domain. Please access it using the panel domain URL.',
		loginSuccessTitle: 'Signed in successfully',
		loginSuccessDesc: 'Welcome back, {username}.',
		recoveryUsedTitle: 'One-Time Recovery Code Used',
		recoveryUsedDesc: 'You signed in using a recovery code. {remaining} recovery code(s) remaining.',
		networkError: 'Unable to connect to the wdev server. Check your network connection or service status.',
		logoutTitle: 'Session Closed',
		logoutDesc: 'You have signed out of the panel.'
	}
};
