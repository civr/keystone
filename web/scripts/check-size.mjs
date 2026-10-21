#!/usr/bin/env node
// Fails the build when the JavaScript needed to render the home page (/)
// exceeds the budget (100 kB gzip, spec speed target).
//
// What is measured: every .js file under build/_app/immutable/ referenced by
// build/200.html (the entry modules), plus the route nodes of "/" (root
// layout and home page, looked up in the client route dictionary), plus
// everything those files import *statically*, followed transitively. Dynamic
// import() targets (other routes, error page) are not counted — they are not
// loaded for "/". Each file counts once, gzip level 9.
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';
import { fileURLToPath } from 'node:url';

const BUDGET = 100 * 1024;
const build = resolve(dirname(fileURLToPath(import.meta.url)), '..', 'build');
const immutable = join(build, '_app', 'immutable');

function fail(msg) {
	console.error(`check-size: ${msg}`);
	process.exit(1);
}

if (!existsSync(join(build, '200.html'))) fail('build/200.html missing — run `npm run build` first');

const roots = new Set();

// 1. Entry modules referenced from the SPA shell.
const shell = readFileSync(join(build, '200.html'), 'utf8');
for (const m of shell.matchAll(/["'(]\/?((?:[^"'()]*\/)?_app\/immutable\/[^"'()]+\.js)["')]/g)) {
	roots.add(join(build, m[1].replace(/^.*?_app\//, '_app/')));
}
if (roots.size === 0) fail('no _app/immutable/*.js referenced from 200.html');

// 2. Route nodes for "/": the root layout (0) plus the page and any layouts
// named by the "/" entry of the client route dictionary, e.g. "/":[3] or
// "/":[3,[2]]. Node numbers shift as routes are added, so they are read from
// the build rather than hard-coded.
const entryDir = join(immutable, 'entry');
const appFile = readdirSync(entryDir).find((f) => f.startsWith('app.') && f.endsWith('.js'));
if (!appFile) fail(`entry/app.*.js not found in ${entryDir}`);
const route = readFileSync(join(entryDir, appFile), 'utf8').match(/"\/":\[(-?\d+)(?:,\[([-\d,]*)\])?/);
if (!route) fail('route "/" not found in the client route dictionary');
const layouts = route[2] ? route[2].split(',').filter(Boolean) : [];
const homeNodes = new Set(['0', route[1], ...layouts].map((n) => String(Math.abs(Number(n)))));
for (const n of homeNodes) {
	const dir = join(immutable, 'nodes');
	const file = readdirSync(dir).find((f) => f.startsWith(`${n}.`) && f.endsWith('.js'));
	if (!file) fail(`route node ${n} not found in ${dir}`);
	roots.add(join(dir, file));
}

// 3. Follow static imports (import … from "x", import "x", export … from "x").
const staticImport = /\b(?:import|export)\s*(?:[^"'();]*?\bfrom\s*)?["']([^"']+\.js)["']/g;
const seen = new Map();
const queue = [...roots];
while (queue.length) {
	const file = queue.pop();
	if (seen.has(file)) continue;
	if (!existsSync(file)) fail(`referenced file missing: ${relative(build, file)}`);
	const src = readFileSync(file);
	seen.set(file, gzipSync(src, { level: 9 }).length);
	for (const m of src.toString('utf8').matchAll(staticImport)) {
		const spec = m[1];
		if (spec.startsWith('.')) queue.push(resolve(dirname(file), spec));
		else if (spec.startsWith('/_app/')) queue.push(join(build, spec));
	}
}

let total = 0;
const rows = [...seen].sort((a, b) => b[1] - a[1]);
for (const [file, size] of rows) {
	total += size;
	console.log(`${(size / 1024).toFixed(1).padStart(7)} kB  ${relative(build, file)}`);
}
const line = `${(total / 1024).toFixed(1)} kB gzip JS for / (${rows.length} files, budget ${BUDGET / 1024} kB)`;
if (total > BUDGET) fail(`OVER BUDGET: ${line}`);
console.log(`check-size: OK — ${line}`);
