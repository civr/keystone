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
