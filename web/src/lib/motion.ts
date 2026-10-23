import type { TransitionConfig } from 'svelte/transition';

/*
 * Motion vocabulary, Apple-style: short, eased, never bouncing (nothing here
 * is thrown by a gesture). Apple's ease for state changes, the iOS sheet
 * curve for anything arriving. Mirrors --ease / --ease-out / --dur-1..4 in
 * theme.css.
 */

/** --dur-1..4 in ms. */
export const DUR = [100, 180, 240, 320] as const;

/** cubic-bezier(x1, y1, x2, y2) as an easing function on [0, 1]. */
export function bezier(x1: number, y1: number, x2: number, y2: number): (t: number) => number {
	const cx = 3 * x1;
	const bx = 3 * (x2 - x1) - cx;
	const ax = 1 - cx - bx;
	const cy = 3 * y1;
	const by = 3 * (y2 - y1) - cy;
	const ay = 1 - cy - by;
	const sx = (u: number) => ((ax * u + bx) * u + cx) * u;
	const sy = (u: number) => ((ay * u + by) * u + cy) * u;
	const dx = (u: number) => (3 * ax * u + 2 * bx) * u + cx;
	return (t: number) => {
		if (t <= 0) return 0;
		if (t >= 1) return 1;
		// Newton first, bisection when the slope is too flat to trust.
		let u = t;
		for (let i = 0; i < 8; i++) {
			const err = sx(u) - t;
			if (Math.abs(err) < 1e-7) return sy(u);
			const d = dx(u);
			if (Math.abs(d) < 1e-6) break;
			u -= err / d;
		}
		let lo = 0;
		let hi = 1;
		u = t;
		for (let i = 0; i < 40; i++) {
			const x = sx(u);
			if (Math.abs(x - t) < 1e-7) break;
			if (x < t) lo = u;
			else hi = u;
			u = (lo + hi) / 2;
		}
		return sy(u);
	};
}

/** --ease: state changes. */
export const ease = bezier(0.25, 0.1, 0.25, 1);
/** --ease-out (decelerate): anything arriving. */
export const easeOut = bezier(0.32, 0.72, 0, 1);

export function reducedMotion(): boolean {
	return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** Arrive: fade in while rising a few px (rows, panels, menus). Reduced motion: none. */
export function rise(
	_node: Element,
	{ delay = 0, y = 4, scale = 1, duration = 220 }: { delay?: number; y?: number; scale?: number; duration?: number } = {}
): TransitionConfig {
	if (reducedMotion()) return { duration: 0 };
	return {
		delay,
		duration,
		easing: easeOut,
		css: (t, u) => `opacity:${t};transform:translateY(${u * y}px) scale(${scale + (1 - scale) * t})`
	};
}

/**
 * Wizard step: a short fade with a small shift toward the direction of travel
 * (dir 1 = forward, -1 = back). Leaving is a quick fade. Reduced motion: none.
 */
export function step(_node: Element, { dir = 1, out = false }: { dir?: number; out?: boolean } = {}): TransitionConfig {
	if (reducedMotion()) return { duration: 0 };
	if (out) return { duration: DUR[1], easing: ease, css: (t) => `opacity:${t}` };
	const dx = dir * 12;
	return {
		duration: DUR[3],
		easing: easeOut,
		css: (t, u) => `opacity:${t};transform:translateX(${u * dx}px)`
	};
}
