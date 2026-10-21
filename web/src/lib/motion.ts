import type { TransitionConfig } from 'svelte/transition';

/** Critically damped spring (damping 1.0) as an easing over a fixed window. */
const OMEGA = 2 * Math.PI / 0.4; // response 0.4 s
const WINDOW = 0.6; // seconds until visually settled
export function spring(t: number): number {
	const x = OMEGA * WINDOW * t;
	return t >= 1 ? 1 : 1 - (1 + x) * Math.exp(-x);
}

export function reducedMotion(): boolean {
	return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/**
 * Materialize: scale + lift + fade on a spring, from transform-origin set by
 * the caller. Reduced motion: a short cross-fade only.
 */
export function pop(
	_node: Element,
	{ delay = 0, scale = 0.94, y = 6, duration = 600 }: { delay?: number; scale?: number; y?: number; duration?: number } = {}
): TransitionConfig {
	if (reducedMotion()) return { delay: 0, duration: 140, css: (t) => `opacity:${t}` };
	return {
		delay,
		duration,
		easing: spring,
		css: (t, u) =>
			`opacity:${Math.min(1, t * 1.6)};transform:translateY(${u * y}px) scale(${scale + (1 - scale) * t})`
	};
}

/**
 * Wizard step: slides in from the side it comes from and leaves the way it
 * goes (dir 1 = forward, -1 = back), on the critically damped spring. Use the
 * same dir for in and out so the path is symmetric. Reduced motion: cross-fade.
 */
export function step(_node: Element, { dir = 1, out = false }: { dir?: number; out?: boolean } = {}): TransitionConfig {
	if (reducedMotion()) return { duration: 160, css: (t) => `opacity:${t}` };
	const dx = (out ? -dir : dir) * 28;
	return {
		duration: out ? 260 : 600,
		easing: out ? (t) => t * t : spring,
		css: (t, u) => `opacity:${out ? t : Math.min(1, t * 1.8)};transform:translateX(${u * dx}px)`
	};
}
