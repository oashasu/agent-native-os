/*---------------------------------------------------------------------------------------------
 *  Copyright (c) Microsoft Corporation. All rights reserved.
 *  Licensed under the MIT License. See License.txt in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

// Human Console shell: two-lens commands, lens context key, layout switching (UI-1 spec appendix F / C.1).

import { registerAction2, Action2 } from '../../../../platform/actions/common/actions.js';
import { ServicesAccessor } from '../../../../platform/instantiation/common/instantiation.js';
import { IContextKeyService, IContextKey, ContextKeyExpr } from '../../../../platform/contextkey/common/contextkey.js';
import { IWorkbenchLayoutService, Parts } from '../../../../workbench/services/layout/browser/layoutService.js';
import { ICommandService } from '../../../../platform/commands/common/commands.js';
import { KeybindingsRegistry, KeybindingWeight } from '../../../../platform/keybinding/common/keybindingsRegistry.js';
import { KeyCode, KeyMod } from '../../../../base/common/keyCodes.js';
import {
	CMD_ENTER_IDE, CMD_ENTER_AGENT, CMD_TOGGLE, CMD_SWITCH_CTX,
	CMD_PREV_SESSION, CMD_NEXT_SESSION, CMD_APPROVE, CMD_REJECT, LensKind
} from '../common/humanConsole.js';

const LENS_KEY = 'humanLens';
let lensKey: IContextKey<LensKind> | undefined;
let curLens: LensKind = 'ide';

function setLens(accessor: ServicesAccessor, lens: LensKind): void {
	curLens = lens;
	if (!lensKey) {
		lensKey = accessor.get(IContextKeyService).createKey<LensKind>(LENS_KEY, lens);
	}
	lensKey.set(lens);

	const layout = accessor.get(IWorkbenchLayoutService);
	const commands = accessor.get(ICommandService);
	if (lens === 'agent') {
		// Agent lens: hide activity/status bars, show sidebar + maximized panel (holds the webview).
		layout.setPartHidden(true, Parts.ACTIVITYBAR_PART);
		layout.setPartHidden(true, Parts.STATUSBAR_PART);
		layout.setPartHidden(false, Parts.SIDEBAR_PART);
		layout.setPartHidden(false, Parts.PANEL_PART);
		commands.executeCommand('workbench.action.toggleMaximizedPanel');
	} else {
		layout.setPartHidden(false, Parts.ACTIVITYBAR_PART);
		layout.setPartHidden(false, Parts.STATUSBAR_PART);
		layout.setPartHidden(false, Parts.SIDEBAR_PART);
		commands.executeCommand('workbench.action.toggleMaximizedPanel');
	}
}

registerAction2(class EnterIdeLens extends Action2 {
	constructor() { super({ id: CMD_ENTER_IDE, title: { value: 'IDE lens', original: 'IDE lens' } }); }
	run(accessor: ServicesAccessor) { setLens(accessor, 'ide'); }
});
registerAction2(class EnterAgentLens extends Action2 {
	constructor() { super({ id: CMD_ENTER_AGENT, title: { value: 'Agent lens', original: 'Agent lens' } }); }
	run(accessor: ServicesAccessor) { setLens(accessor, 'agent'); }
});
registerAction2(class ToggleLens extends Action2 {
	constructor() { super({ id: CMD_TOGGLE, title: { value: 'Toggle lens', original: 'Toggle lens' } }); }
	run(accessor: ServicesAccessor) { setLens(accessor, curLens === 'agent' ? 'ide' : 'agent'); }
});

// Switch work context (S3 wires the work index; S1 keeps a stub).
registerAction2(class SwitchContext extends Action2 {
	constructor() { super({ id: CMD_SWITCH_CTX, title: { value: 'Switch work context', original: 'Switch work context' } }); }
	run(_accessor: ServicesAccessor) {
		console.log('[human-console] switchContext (mock; S3 wires work index)');
	}
});

// Keybindings (UI-1 spec appendix C.4): lens keys + agent-lens keys gated on context.
const W = KeybindingWeight.WorkbenchContrib;
const AGENT_WHEN = ContextKeyExpr.deserialize('humanLens == agent');
KeybindingsRegistry.registerKeybindingRule({ id: CMD_ENTER_IDE, weight: W, primary: KeyMod.CtrlCmd | KeyCode.Digit1 });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_ENTER_AGENT, weight: W, primary: KeyMod.CtrlCmd | KeyCode.Digit2 });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_TOGGLE, weight: W, primary: KeyCode.F4 });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_SWITCH_CTX, weight: W, primary: KeyMod.CtrlCmd | KeyCode.KeyK });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_PREV_SESSION, weight: W, primary: KeyCode.BracketLeft, when: AGENT_WHEN });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_NEXT_SESSION, weight: W, primary: KeyCode.BracketRight, when: AGENT_WHEN });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_APPROVE, weight: W, primary: KeyCode.KeyA, when: AGENT_WHEN });
KeybindingsRegistry.registerKeybindingRule({ id: CMD_REJECT, weight: W, primary: KeyCode.KeyR, when: AGENT_WHEN });