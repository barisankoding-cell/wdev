class TerminalStore {
	activated = $state(false);
	connectedCount = $state(0);

	activate() {
		this.activated = true;
	}

	setConnectedCount(count: number) {
		this.connectedCount = Math.max(0, count);
	}

	reset() {
		this.activated = false;
		this.connectedCount = 0;
	}
}

export const terminalStore = new TerminalStore();
