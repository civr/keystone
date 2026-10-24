# Keystone

Zelf te hosten identity provider (SSO) voor je organisatie, met Active Directory als bron.

## Installeren

```sh
git clone <repo-url> keystone && cd keystone
docker compose up -d
docker compose logs core | grep "setup code"
```

Open daarna `http://<host>:8080` en doorloop de installatiewizard met de setup code uit de logs.
Een andere poort kies je met `KEYSTONE_PORT=9000 docker compose up -d`.

De images komen standaard van `ghcr.io/civr/keystone-*`. Is de repository privé, log dan eerst in met
`docker login ghcr.io` (of maak de packages publiek). Zonder registry werkt `docker compose up -d --build`
ook: dan worden alle images lokaal gebouwd.

Bij de eerste start genereert de `init`-container de geheimen (database- en valkey-wachtwoord, sessie-,
data- en kluissleutel) in het volume `keystone_secrets`. Bestaande geheimen worden nooit overschreven.

## Reverse proxy

Keystone praat zelf geen TLS. Zet een reverse proxy ervoor, bijvoorbeeld met `keystone.<domein>` naar
poort 8080 van deze host, en laat TLS daar eindigen. De proxy moet `X-Forwarded-For` doorgeven. Poort 8080
van de `core`-container wordt bewust nooit gepubliceerd.

Voorbeeld (Caddy op de proxy-host):

```
keystone.voorbeeld.nl {
	reverse_proxy 127.0.0.1:8080
}
```

### Vertrouwde proxy's instellen

Keystone gelooft `X-Forwarded-For` alleen van de eigen `web`-container en van adressen die je zelf als
vertrouwde proxy opgeeft. Zonder die instelling ziet Keystone elke bezoeker met het adres van je
reverse proxy: rate limits, blokkades en de toegestane IP-bereiken gelden dan voor iedereen tegelijk.

1. Log in via de reverse proxy en open **Instellingen › Beveiliging › Netwerk**. Daar staat
   *Jouw adres volgens Keystone* (hetzelfde adres zie je bij je sessie onder **Instellingen › Apparaten**).
2. Is dat het adres van de proxy in plaats van je eigen apparaat, voeg het dan toe aan **Vertrouwde proxy's**.

Draait de proxy op dezelfde host en verbindt hij met de gepubliceerde poort, dan komt hij meestal binnen
via de Docker-gateway van het compose-netwerk, bijvoorbeeld `172.18.0.1`. Zo vind je die:

```sh
docker network inspect keystone_default --format '{{(index .IPAM.Config 0).Gateway}}'
```

en voeg je `172.18.0.1/32` toe. Draait de proxy op een andere machine, gebruik dan het IP-adres van die
machine. Na het opslaan toont *Jouw adres volgens Keystone* je echte adres.

Vul in de installatiewizard het https-adres van de proxy in als publiek adres: passkeys werken alleen via
https (of `localhost`). Heb je per ongeluk een verkeerd adres opgegeven, open de wizard dan opnieuw via
`http://<host>:8080/setup`; tot de installatie is afgerond kun je de domeinstap daar opnieuw doen.

Wil je dat de meegeleverde `web`-container zelf certificaten ophaalt, haal dan `auto_https off` uit
`deploy/Caddyfile`, vervang `:80` door je hostnaam en publiceer ook poort 443 (en geef Caddy een
persistent volume voor `/data`).

## Apps koppelen

Voeg een app toe onder **Instellingen › Apps** en kies hoe Keystone inlogt. Na opslaan toont het venster
de gegevens die de app nodig heeft, met een kopieerknop. Onder *Account in de app* vul je in als wie
Keystone je bij die app aanmeldt (leeg = je eigen AD-gegevens).

### Bastion (SAML)

In Keystone: protocol **SAML**, entity ID `https://bastion.<domein>/api/auth/saml/metadata`, ACS-URL
`https://bastion.<domein>/api/auth/saml/acs`, adres van de tegel `https://bastion.<domein>/api/auth/saml/start`.
In Bastion: *Beheer › Inloggen › Keystone (SAML)* aan, metadata-URL `https://keystone.<domein>/saml/metadata`.
Bastion vraagt na de eerste SAML-login één keer je AD-wachtwoord voor de sessiehost.

### Project Nimbus (OpenID Connect)

In Keystone: protocol **OpenID Connect**, redirect-URI `https://nimbus.<domein>/api/auth/sso/callback`,
adres van de tegel `https://nimbus.<domein>/api/auth/sso/redirect`; maak daarna een client-secret aan.
In Nimbus: *Instellingen › SSO*, provider **OpenID Connect**, issuer `https://keystone.<domein>`, plus
client-ID en -secret uit Keystone. PKCE is verplicht; Nimbus gebruikt het automatisch.

### Jellyfin (LDAP)

In Keystone: protocol **LDAP**, naam `jellyfin`, maak een bind-wachtwoord aan. In de Jellyfin LDAP-plugin:

| Instelling | Waarde |
|---|---|
| LDAP Server | de Keystone-host, poort `636`, *Secure LDAP* aan |
| Bind User | `cn=bind,ou=jellyfin,dc=keystone` + het bind-wachtwoord |
| Base DN | `ou=jellyfin,dc=keystone` |
| User Search Filter | `(objectClass=inetOrgPerson)` |
| Login attribute / filter | `uid` / `(uid={username})` |

keystone-ldap heeft een eigen, zelfondertekend certificaat (download onder *Beveiliging › Sleutels*).
Vertrouw het in Jellyfin of zet *Skip SSL verification* aan. Je wachtwoord wordt bij elke login tegen AD
gecontroleerd; is AD onbereikbaar, dan werkt inloggen in Jellyfin tijdelijk niet.

### Home Assistant (poortwachter)

In Keystone: protocol **Poortwachter**, hostnaam `ha.<domein>`, uitzonderingen via het voorstel
*Home Assistant* (de app op je telefoon gebruikt eigen tokens). Je reverse proxy vraagt `keystone-gate`
(poort 8081) bij elk verzoek of het mag. De poortwachter-cookie geldt voor het hele domein dat je in de
setup opgaf, dus Keystone en de apps moeten daaronder hangen.

Traefik:

```yaml
http:
  middlewares:
    keystone:
      forwardAuth:
        address: http://keystone-host:8081/verify
        authResponseHeaders: [X-Keystone-User, X-Keystone-Email, X-Keystone-Name]
```

Caddy:

```
ha.voorbeeld.nl {
	request_header -X-Keystone-*
	forward_auth keystone-host:8081 {
		uri /verify
		copy_headers X-Keystone-User X-Keystone-Email X-Keystone-Name
	}
	reverse_proxy homeassistant:8123
}
```

nginx:

```
location = /_keystone { internal; proxy_pass http://keystone-host:8081/verify?mode=nginx;
  proxy_set_header X-Original-URL $scheme://$http_host$request_uri; proxy_pass_request_body off; proxy_set_header Content-Length ""; }
location @keystone_login { proxy_pass http://keystone-host:8081/login-redirect?rd=$scheme://$http_host$request_uri; }
location / { auth_request /_keystone; error_page 401 = @keystone_login;
  auth_request_set $ks_user $upstream_http_x_keystone_user;
  auth_request_set $ks_email $upstream_http_x_keystone_email;
  auth_request_set $ks_name $upstream_http_x_keystone_name;
  proxy_set_header X-Keystone-User $ks_user;
  proxy_set_header X-Keystone-Email $ks_email;
  proxy_set_header X-Keystone-Name $ks_name;
  proxy_pass http://homeassistant:8123; }
```

Alle drie de `proxy_set_header`-regels zijn nodig: nginx vervangt zo wat een bezoeker zelf meestuurt, en
laat de header weg als de poortwachter hem niet zet (bij een uitzondering).

Laat alleen je reverse proxy bij poort 8081 en zorg dat hij `X-Keystone-*` van bezoekers nooit doorgeeft
(Traefik vervangt ze vanzelf; bij Caddy doet `request_header -X-Keystone-*` dat). Voor automatisch inloggen
in Home Assistant zelf installeer je de component in `integrations/home-assistant` (zie de README daar).

### UniFi OS (kluis)

Protocol **Kluis**, met gebruikersnaam en wachtwoord van je UniFi-account. Een klik op de tegel kopieert
het wachtwoord (na een passkey-bevestiging als de vorige langer dan 15 minuten geleden was) en opent UniFi;
het klembord wordt na 30 seconden gewist. Het wachtwoord staat versleuteld met `vault_key` uit het
secrets-volume, niet leesbaar in de database.

## Updaten

```sh
docker compose pull && docker compose up -d
```

## Buitengesloten? Netwerkbeperkingen herstellen

Heb je jezelf buitengesloten met de netwerkbeperkingen (toegestane IP-bereiken), dan wis je ze met:

```sh
docker compose exec core keystone-core reset-network
```

## Back-up

Maak een back-up van de twee volumes: `keystone_db` (Postgres) en `keystone_secrets` (sleutels en
wachtwoorden). Zonder `keystone_secrets` is de database niet meer te ontsleutelen; bewaar ze apart en veilig.

Het volume `keystone_valkey` bevat alleen sessies en tellers voor rate limits. Een back-up is niet nodig:
raak je het kwijt, dan log je opnieuw in.

## Beveiligingsnotities

- Het valkey-wachtwoord staat in geen enkele omgevingsvariabele, maar wel in de opdrachtregel van
  `valkey-server` binnen de valkey-container (`--requirepass`). Wie in die container processen kan bekijken
  (of op de host `docker top` mag gebruiken), kan het lezen. Valkey is alleen bereikbaar
  binnen het compose-netwerk en bevat alleen sessies; geef Docker-toegang op de host dus niet aan anderen.

## Ontwikkelen

`make test`, `make web-test` en `bash scripts/smoke.sh` (volledige compose-rooktest, ruimt zichzelf op).

Bouw je de images zelf, gebruik dan voor `core` en `web` dezelfde `KEYSTONE_VERSION` (build-arg, standaard
`dev`; `docker compose build` regelt dat). `core` bevat de startpagina die naar de bestanden in `web` verwijst;
met verschillende waarden blijft de pagina leeg.
