import { describe, expect, it } from 'vitest';
import * as motion from './motion';
import { bezier, ease, easeOut } from './motion';

describe('bezier', () => {
	it('hits both endpoints', () => {
		for (const f of [ease, easeOut]) {
			expect(f(0)).toBe(0);
			expect(f(1)).toBe(1);
		}
	});

	it('is monotonic and never overshoots (no bounce, no elastic)', () => {
		for (const f of [ease, easeOut]) {
			let prev = 0;
			for (let i = 1; i <= 200; i++) {
				const y = f(i / 200);
				expect(y).toBeGreaterThanOrEqual(prev - 1e-9);
				expect(y).toBeLessThanOrEqual(1 + 1e-9);
				prev = y;
			}
		}
	});

	it('--ease is symmetric: halfway in time is halfway in value', () => {
		expect(ease(0.5)).toBeCloseTo(0.5, 4);
	});

	it('--ease-out decelerates: most of the way there early', () => {
		expect(easeOut(0.25)).toBeGreaterThan(0.75);
	});

	it('linear control points give a straight line', () => {
		const lin = bezier(0, 0, 1, 1);
		expect(lin(0.3)).toBeCloseTo(0.3, 4);
	});
});

describe('motion vocabulary', () => {
	it('exports no spring easings', () => {
		expect('spring' in motion).toBe(false);
	});

	it('durations follow --dur-1..4', () => {
		expect(motion.DUR).toEqual([100, 150, 200, 250]);
	});
});
