export type ProxyKind = 'npm' | 'nginx' | 'caddy' | 'traefik';

type Options = { host: string; gate: string; keystone: string; signIn: string; header?: string };

/** Reverse-proxy snippet that asks keystone-gate on every request (see README, gate). */
export function proxyConfig(kind: ProxyKind, o: Options): string {
	const server = ['form', 'json', 'jellyfin'].includes(o.signIn);
	const header = o.signIn === 'basic' ? 'Authorization' : o.signIn === 'header' && o.header ? o.header : '';
	const out = ['X-Keystone-User', 'X-Keystone-Email', 'X-Keystone-Name', ...(header ? [header] : [])];

	if (kind === 'caddy') {
		const auth = `forward_auth ${o.gate} {\n\turi /verify\n\tcopy_headers ${out.join(' ')}\n}\nreverse_proxy APP_ADDRESS`;
		const body = server ? `handle /.keystone/* {\n\treverse_proxy ${o.gate}\n}\nhandle {\n${indent(auth)}\n}` : auth;
		return `${o.host} {\n\trequest_header -X-Keystone-*\n${indent(body)}\n}\n`;
	}

	if (kind === 'traefik') {
		const lines = [
			'http:',
			'  middlewares:',
			'    keystone:',
			'      forwardAuth:',
			`        address: http://${o.gate}/verify`,
			`        authResponseHeaders: [${out.join(', ')}]`,
			'  routers:',
			'    app:',
			`      rule: Host(\`${o.host}\`)`,
			'      middlewares: [keystone]',
			'      service: app',
			...(server
				? ['    keystone-gate:', `      rule: Host(\`${o.host}\`) && PathPrefix(\`/.keystone/\`)`, '      priority: 100', '      service: keystone-gate']
				: []),
			'  services:',
			'    app:',
			'      loadBalancer:',
			'        servers:',
			'          - url: http://APP_ADDRESS',
			...(server ? ['    keystone-gate:', '      loadBalancer:', '        servers:', `          - url: http://${o.gate}`] : [])
		];
		return lines.join('\n') + '\n';
	}

	const lines = [
		'location = /_keystone {',
		'\tinternal;',
		`\tproxy_pass http://${o.gate}/verify?mode=nginx;`,
		'\tproxy_set_header X-Original-URL $scheme://$http_host$request_uri;',
		'\tproxy_pass_request_body off;',
		'\tproxy_set_header Content-Length "";',
		'}',
		'location @keystone_login {',
		`\treturn 302 ${o.keystone}/api/gate/start?rd=$scheme://$http_host$request_uri;`,
		'}',
		...(server
			? [
					'location /.keystone/ {',
					`\tproxy_pass http://${o.gate};`,
					'\tproxy_set_header Host $http_host;',
					'\tproxy_set_header X-Forwarded-Host $http_host;',
					'\tproxy_set_header X-Forwarded-Proto $scheme;',
					'}'
				]
			: []),
		'location / {',
		'\tauth_request /_keystone;',
		'\terror_page 401 = @keystone_login;',
		...['user', 'email', 'name'].map((k) => `\tauth_request_set $ks_${k} $upstream_http_x_keystone_${k};`),
		...['User', 'Email', 'Name'].map((k) => `\tproxy_set_header X-Keystone-${k} $ks_${k.toLowerCase()};`),
		...(header
			? [
					`\tauth_request_set $ks_extra $upstream_http_${header.toLowerCase().replaceAll('-', '_')};`,
					`\tproxy_set_header ${header} $ks_extra;`
				]
			: []),
		kind === 'npm' ? '\tinclude conf.d/include/proxy.conf;' : '\tproxy_pass http://APP_ADDRESS; # the app',
		'}'
	];
	const body = lines.join('\n');
	return (kind === 'npm' ? body : `server {\n\tserver_name ${o.host};\n${indent(body)}\n}`) + '\n';
}

const indent = (s: string) => s.replace(/^(?=.)/gm, '\t');
