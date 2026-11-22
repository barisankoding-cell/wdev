import { getContext, setContext } from 'svelte';

const DROPDOWN_MENU_CONTROLLER_KEY = Symbol('wdev:dropdown-menu-controller');

export interface DropdownMenuController {
	close: () => void;
}

export function setDropdownMenuController(controller: DropdownMenuController): void {
	setContext(DROPDOWN_MENU_CONTROLLER_KEY, controller);
}

export function getDropdownMenuController(): DropdownMenuController | undefined {
	return getContext<DropdownMenuController | undefined>(DROPDOWN_MENU_CONTROLLER_KEY);
}
