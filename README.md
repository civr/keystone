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

Bij de eerste start genereert de `init`-container de geheimen (database- en valkey-wachtwoord, sessie- en
datasleutel) in het volume `keystone_secrets`. Bestaande geheimen worden nooit overschreven.

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

Wil je dat de meegeleverde `web`-container zelf certificaten ophaalt, haal dan `auto_https off` uit
`deploy/Caddyfile`, vervang `:80` door je hostnaam en publiceer ook poort 443 (en geef Caddy een
persistent volume voor `/data`).

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

## Ontwikkelen

`make test`, `make web-test` en `bash scripts/smoke.sh` (volledige compose-rooktest, ruimt zichzelf op).
