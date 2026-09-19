/*---------------------------------------------------------------------------------------------
 *  Copyright (c) Microsoft Corporation. All rights reserved.
 *  Licensed under the MIT License. See License.txt in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

import { Event, Emitter } from '../../../../base/common/event.js';

// Human Console shell (UI-1 spec appendix F): command ids, lens kinds, SpineProvider mock.

export const CMD_ENTER_IDE = 'human.enterIdeLens';
export const CMD_ENTER_AGENT = 'human.enterAgentLens';
export const CMD_TOGGLE = 'human.toggleLens';
export const CMD_SWITCH_CTX = 'human.switchContext';
export const CMD_PREV_SESSION = 'human.prevSession';
export const CMD_NEXT_SESSION = 'human.nextSession';
export const CMD_APPROVE = 'human.approveSelected';
export const CMD_REJECT = 'human.rejectSelected';

export type LensKind = 'ide' | 'agent';

// SpineProvider contract (UI-2 swaps in the Go process channel; surface stays).
export interface SpineContext {
	ws: string;
	branch: string;
	dirty: boolean;
	title: string;
	stage: string;
}
export type MessageCard =
	| { k: 'plan' | 'think' | 'edit' | 'test' | 'review'; h: string; b: string }
	| { k: 'tool'; h: string; b: string; needApprove?: boolean; approved?: boolean; approvedBy?: string }
	| { k: 'user'; h: string; b: string };
export interface AgentSession {
	id: string;
	provider: string;
	status: 'ACTIVE' | 'WAITING' | 'DETACHED' | 'ARCHIVED';
	started: string;
	transcript: MessageCard[];
}

export interface SpineProvider {
	listContexts(): SpineContext[];
	getContext(id: string): SpineContext & { sessions: AgentSession[] };
	onContextChanged(cb: () => void): void;
	switchSession(ctxId: string, sessionId: string): void;
	approveTool(ctxId: string, sessionId: string, cardIdx: number, mode: 'once' | 'always'): void;
	stopAgent(ctxId: string, sessionId: string): void;
	sendPrompt(ctxId: string, sessionId: string, text: string): void;
	revertFile(ctxId: string, sessionId: string, file: string): void;
}

// In-memory mock (S2 seeds it with the prototype CTX: t17 / t23 / t31).
export class MockSpine implements SpineProvider {
	private readonly _onChange = new Emitter<void>();
	readonly onChange: Event<void> = this._onChange.event;

	private ctxs: SpineContext[] = [
		{ ws: '/work/t17/payment-service', branch: 'T17-impl', dirty: true, title: 'PaymentService overflow fix', stage: 'IN_REVIEW' },
		{ ws: '/work/t23/batch-check', branch: 'feat/t23', dirty: true, title: 'BatchCheck idempotency test', stage: 'REVIEW' },
		{ ws: '/work/t31/jpa-audit', branch: 'T31-audit', dirty: false, title: 'JPA audit log', stage: 'PLANNED' }
	];

	listContexts(): SpineContext[] { return this.ctxs; }
	getContext(_id: string): SpineContext & { sessions: AgentSession[] } {
		return { ...this.ctxs[0], sessions: [] };
	}
	onContextChanged(cb: () => void): void { this.onChange(cb); }
	switchSession(): void { this._onChange.fire(); }
	approveTool(): void { this._onChange.fire(); }
	stopAgent(): void { this._onChange.fire(); }
	sendPrompt(): void { this._onChange.fire(); }
	revertFile(): void { this._onChange.fire(); }
}