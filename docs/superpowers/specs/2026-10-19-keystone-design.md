# Keystone — ontwerp

Datum: 2026-10-19 · Eigenaar: Thijs

## 1. Wat en waarom

Keystone is een zelf-gehoste identity provider voor één persoon, in de geest van Keycloak/Authentik maar zo simpel en mooi als UniFi Identity. Je logt één keer in (AD-wachtwoord + passkey) en komt op een startpagina met al je apps. Van daaruit kom je zonder opnieuw in te loggen in elke app, als het juiste account in die app.

Waarom: Thijs heeft eigen (vibe coded) apps en homelab-diensten, elk met losse accounts. Keycloak en Authentik kunnen het, maar voelen zwaar en lelijk. Keystone moet aanvoelen als een product van Apple × Microsoft: modern, snel en zonder gedoe.

## 2. Doelen, niet-doelen, anti-doelen

**Doelen**
- Eén login (AD + passkey) → toegang tot alle apps.
- Startpagina in het **Project Nimbus-thema** (zie §6): groet, zoekbalk, app-tegels, wit op een licht canvas.
- Per app een **accountkoppeling**: Keystone-gebruiker `thijs` → in app 1 `t.willems`, in app 2 `civr`. Keystone stuurt de juiste naam mee.
- Protocollen naar apps: **SAML 2.0 IdP**, **OIDC provider**, **LDAP-server** (proxy naar AD) en **forward auth** (poortwachter achter reverse proxy).
- **Kluis** voor app-credentials, alleen voor apps die geen SAML/LDAP/OIDC kunnen.
- Draait als Docker-containers via één `docker compose`.

**Niet-doelen**
- Meerdere gebruikers, groepen of rechtenbeheer. Er is één gebruiker: Thijs.
- Zelf een gebruikersdatabase zijn. AD is de bron van waarheid, behalve het nood-account.
- Volwaardige wachtwoordmanager of browserextensie (v1).
- SCIM/provisioning, account aanmaken in apps.

**Anti-doelen** (falen, ook als het technisch werkt)
- **Traag.** Elke wachttijd die je merkt is een bug.
- **Vaak invullen.** Steeds opnieuw inloggen of formulieren invullen.
- **Lelijk of generiek.** Moet voelen als een Apple/Microsoft-product, niet als een admin-paneel.
- Jezelf buitensluiten (AD down, passkey kwijt).

## 3. Eerste apps

| App | Wat | Koppeling |
|---|---|---|
| Bastion | eigen app, eigen accounts, kan LDAP + SAML | **SAML** (voorkeur) |
| Project Nimbus | eigen app (`/Volumes/DATA/Software/proxmox-cloud`, FastAPI + React), eigen accounts; heeft OIDC maar hardcoded op Microsoft Entra | **OIDC** — Nimbus wordt aangepast naar generieke OIDC (issuer-URL/discovery) |
| Jellyfin | media | **LDAP** via `keystone-ldap` (LDAP-plugin); optioneel forward auth ervoor |
| Home Assistant | domotica | **forward auth** via `keystone-gate`; geen native SAML/OIDC |
| UniFi OS Server | netwerk | **tegel + kluis** (opgeslagen login), optioneel forward auth ervoor |

Elke app krijgt in Keystone: naam, icoon/kleur, URL, protocol, protocolinstellingen en de gekoppelde app-gebruikersnaam (+ attributen zoals e-mail en weergavenaam die meegestuurd worden).

## 4. Inloggen & sessies

- **Bron:** AD via **LDAPS** (bind met service-account voor lookup, daarna bind als gebruiker voor wachtwoordcontrole). AD hoeft geen SAML/ADFS te hebben.
- **Eerste login op een apparaat:** gebruikersnaam + AD-wachtwoord → passkey (WebAuthn: Face ID / Touch ID / Windows Hello). Bij allereerste login: passkey registreren.
- **Weinig invullen (mijn keuze):**
  - Sessie: 30 dagen, schuivend (verlengt bij gebruik).
  - Vertrouwd apparaat: na afloop van de sessie volstaat **alleen passkey** (geen wachtwoord), tot 90 dagen na de laatste volledige login.
  - Gebruikersnaam onthouden; passkey-prompt automatisch openen (conditional UI / autofill).
  - Klik op een app-tegel → direct de app in, geen tussenscherm (SAML IdP-initiated of SP-initiated met automatische POST).
- **LDAP-apps** (Jellyfin) krijgen alleen gebruikersnaam + wachtwoord: `keystone-ldap` vertaalt de app-gebruikersnaam terug naar `thijs` en checkt het wachtwoord tegen AD. Passkey is daar technisch niet mogelijk (bekend en geaccepteerd).
- **Noodtoegang:** bij installatie 10 eenmalige herstelcodes + een lokaal nood-account (eigen wachtwoord + passkey) dat werkt als AD onbereikbaar is.

## 5. Kluis

- Alleen voor apps zonder SAML/LDAP/OIDC (nu: UniFi OS).
- Slaat app-gebruikersnaam + wachtwoord op, versleuteld (AES-256-GCM / XChaCha20) met een sleutel uit een Docker secret of env-bestand, **niet** in de database.
- Op de tegel: één klik → app openen + wachtwoord naar het klembord (wordt na 30 s gewist). Gebruikersnaam ook kopieerbaar.
- Tonen/kopiëren van een wachtwoord vereist een sessie jonger dan X minuten, anders eerst een passkey-bevestiging (mijn keuze: 15 min).

## 6. Uiterlijk (Project Nimbus-thema)

Op verzoek van Thijs (22-10-2026: "kijk naar het thema van project nimbus … dat thema wil ik hebben") vervangt het thema van Project Nimbus de eerder goedgekeurde stijl A (radiaal verloop, glas, veerbeweging, donkere modus). Bron: `proxmox-cloud/DESIGN.md` en `frontend/src/index.css`; tokens staan in `web/src/lib/theme.css`.

- Eén licht thema, geen donkere modus. Een lichtblauwgrijs canvas (`#f5f7fb`) waar balk, menu en pagina's op staan; witte kaarten met hoeken van 20px, een bijna onzichtbare rand en een heel lichte schaduw. Geen verlopen, geen glas (`backdrop-filter`), geen geneste kaarten.
- Eén accentkleur, blauw `#0b5cff` (`--primary`), alleen voor de primaire knop, de actieve menuknop, focus en links.
- Lettertype Plus Jakarta Sans, zelf gehost onder `/fonts/` (CSP `'self'`, werkt zonder internet). Paginatitels 1.5rem/800, kaarttitels 1rem/700, labels 0.75rem/600 in zinsnotatie.
- App-shell: een balk van 64px op het canvas met links het logoblok "◆ Keystone" (zo breed als het menu, in de menukleur), een witte zoekbalk van 300px met vergrootglas en `Ctrl K`/`⌘K`, en rechts het gebruikersmenu (initialen, vetgedrukte naam). Een wit menu links met "Mijn apps" en de groep "Instellingen"; het actieve item is een gevulde blauwe pil met witte tekst. Het menu klapt in tot iconen; op een telefoon wordt het een lade die je vanuit de balk opent.
- Startpagina: groet ("Goedemiddag, Thijs") als paginatitel met de datum eronder, daaronder de apps als witte tegels (12px-hoeken) met een getint icoon in de app-kleur, de naam en de host. Bij hover komt een tegel 2px omhoog. Zoeken filtert zoals voorheen (`⌘K`/`Ctrl+K` en `/` focussen, Enter opent de eerste treffer alleen als er een zoekterm is, Esc wist).
- Inloggen en installatie: één witte kaart van 420px midden op het canvas met het logo bovenaan, Nimbus-velden en een volle primaire knop. De installatiestappen tonen de voortgang als rustige getinte pillen.
- Instellingen: per groep een witte kaart met rijen gescheiden door een dunne lijn; schakelaars, knoppen, chips en dialogen volgen Nimbus (dialoog: 16px-hoeken, komt 12px omhoog vanaf 98% schaal in 250 ms).
- Beweging is functioneel en kort (100–250 ms, `--ease` / `--ease-out`), zonder vering of stuiter; `prefers-reduced-motion` zet alles uit.
- Werkt ook op telefoon.

**Instellingen-onderdelen:** AD-verbinding (met testknop), Apps (toevoegen/bewerken, met per protocol een begeleid formulier en kopieerbare metadata-URL's), Accountkoppelingen (per app), Kluis, Beveiliging (passkeys, herstelcodes, sessies/apparaten), Activiteit (recente logins).

## 7. Architectuur (mijn keuze, door Thijs overgelaten)

Losse containers, één `docker-compose.yml`:

| Container | Taak | Tech |
|---|---|---|
| `keystone-web` | startpagina, login, Instellingen (statisch) | SvelteKit (static adapter), geserveerd door Caddy |
| `keystone-core` | API, AD-auth, sessies, WebAuthn, SAML IdP, OIDC provider, kluis | Go |
| `keystone-ldap` | LDAP-server voor apps, vertaalt app-naam → AD-bind | Go |
| `keystone-gate` | forward auth endpoint voor Traefik/Caddy/Nginx | Go |
| `postgres` | config, apps, koppelingen, passkeys, kluis (versleuteld), audit | Postgres 17 |
| `valkey` | sessies, cache van AD-lookups, rate limiting | Valkey |

Eerlijke noot aan Thijs (gecommuniceerd): de snelheid komt vooral van Go + caching. De opsplitsing levert onafhankelijk herstarten, updaten en schalen op.

**Snelheidsdoelen** (meetbaar, onderdeel van "klaar"):
- Startpagina: eerste weergave < 300 ms op LAN; JS-bundle startpagina < 100 kB gzip.
- Klik op tegel → app geladen met ingelogde sessie: Keystone-deel < 150 ms server-side.
- `keystone-gate` auth-check: < 5 ms p95 (sessie uit Valkey).
- `keystone-ldap` bind: < 50 ms p95 met warme AD-verbinding (connection pool).
- Container-images klein (distroless/scratch voor Go).

## 8. Beveiliging

- Alles via HTTPS (Keystone draait achter de reverse proxy van Thijs, of Caddy in `keystone-web` regelt TLS zelf; builder kiest één standaard en documenteert de andere).
- LDAPS naar AD; `keystone-ldap` biedt LDAPS aan apps.
- SAML: ondertekende assertions; signing-sleutel gegenereerd bij eerste start, roteerbaar.
- Rate limiting en lockout op login en LDAP-bind.
- Audit-log van logins, app-toegang en kluisgebruik.
- Kluissleutel en secrets buiten de database (Docker secrets).

## 9. Keuzes van Claude (na builder-check)

- **Toegang:** alleen een geconfigureerde allow-list (standaard één AD-account: dat van Thijs) mag inloggen. Andere AD-accounts worden geweigerd. Bij installatie wordt dat account vastgelegd; de eerste passkey-registratie kan alleen via de setup-wizard met een eenmalige setup-token uit de containerlogs.
- **OIDC:** in v1, met Project Nimbus als eerste echte client (zie §10).
- **AD onbereikbaar:** het nood-account geeft toegang tot Keystone + kluis. `keystone-ldap` valt **niet** terug op gecachete wachtwoorden; LDAP-apps (Jellyfin) werken dan tijdelijk niet. Geaccepteerd.
- **Herstelcodes:** vervangen alleen de passkey-stap (bij verlies), niet het wachtwoord. Na gebruik moet een nieuwe passkey geregistreerd worden.

## 10. Antwoorden van Thijs (na builder-check)

- **Home Assistant: automatisch inloggen = ja.** Klik op tegel → ingelogd in HA zonder HA-login. Vereist een HA custom component (header-based auth via `keystone-gate`); onderdeel van de levering, met installatie-instructie.
- **Eén hoofddomein = ja.** Keystone en alle apps hangen onder één parent domain (bv. `*.home.lan`); het domein wordt bij de setup ingevuld. `keystone-gate` gebruikt een gedeelde sessie-cookie op dat domein.
- **Bastion en Nimbus: code is beschikbaar**: Bastion = `../seamless`, Nimbus = `/Volumes/DATA/Software/proxmox-cloud`. Hun SAML-kant mag worden aangepast zodat één klik direct binnen brengt.
- **Nieuw beginnen.** De eerdere poging `../Auth.i-oud` (Node/TS, OIDC + forward auth) is oud; Keystone wordt opnieuw gebouwd volgens dit ontwerp (Go). `Auth.i-oud` mag als naslag dienen.

**Bevindingen + keuzes van Claude:**
- **Bastion heeft het AD-wachtwoord nodig** om door te loggen op de RDS-sessiehost; SAML levert geen wachtwoord. Bastion vraagt in SAML-modus daarom **één keer** het AD-wachtwoord en onthoudt het (bestaand ontwerp in `../seamless/docs/superpowers/specs/2026-09-21-environments-and-sso-design.md`). Dat blijft zo; Keystone stuurt via SAML de gekoppelde AD-gebruikersnaam mee.
- **Nimbus heeft geen SAML/LDAP**, wel OIDC dat hardcoded naar `login.microsoftonline.com` wijst (`backend/app/routers/auth.py`). Keuze: Nimbus aanpassen naar generieke OIDC (issuer-URL + discovery), Keystone is dan de provider. Eén klik → direct binnen.
- **Native apps** (HA-app, Jellyfin-apps) mogen `keystone-gate` passeren met hun eigen tokens: per app configureerbare uitzonderingen (paden/headers), standaard ingevuld voor HA (`/api/`, `/auth/token`) en Jellyfin.

## 11. Deploy & go

- `git clone` → `docker compose up -d` → klaar. **Niets vooraf invullen**: geen `.env` nodig.
- Een `init`-container genereert bij de eerste start alle secrets (DB-wachtwoord, sessiesleutel, kluissleutel, SAML/OIDC-signing-keys) en bewaart ze in een eigen volume, gescheiden van de database.
- Eerste bezoek → setup-wizard (Nimbus-thema), ontgrendeld met een eenmalige setup-code uit `docker compose logs`. Wizard: domein → AD-verbinding (met test) → eigen AD-account kiezen → passkey registreren → herstelcodes tonen → startpagina.
- Images worden via CI gebouwd en gepubliceerd (GHCR), zodat `docker compose pull && up -d` updatet. Migraties draaien automatisch.

## 12. Beveiliging hardenen via de GUI

Elke beveiligingsmaatregel is instelbaar onder **Instellingen → Beveiliging** (geen config-bestanden of env-vars nodig). Veilige standaarden staan aan; de GUI toont per instelling uitleg en waarschuwt bij verzwakken. Wijzigingen worden gelogd in de audit-log en vereisen een recente passkey-bevestiging.

Minimaal instelbaar:
- **Sessies:** sessieduur, schuivend aan/uit, duur vertrouwd apparaat, idle-timeout, actieve sessies/apparaten bekijken en intrekken.
- **Inloggen:** passkey verplicht (aan/uit, standaard aan), wachtwoord + passkey of passkey-only op vertrouwd apparaat, user verification vereist.
- **Brute-force:** rate limits per IP en per account (login, LDAP-bind, gate), lockout-drempel en -duur.
- **Netwerk:** toegestane IP-reeksen/CIDR's voor login en voor Instellingen apart, trusted proxies.
- **HTTP-headers:** HSTS (incl. preload), CSP, cookie-flags (Secure, SameSite), frame-opties.
- **TLS/LDAP:** minimale TLS-versie, LDAPS verplicht naar AD, CA-certificaat, LDAPS verplicht voor apps op `keystone-ldap`, toegestane LDAP-clients (IP).
- **Sleutels:** SAML/OIDC-signing-keys en kluissleutel roteren, token-levensduur (OIDC access/ID/refresh).
- **Kluis:** herbevestigingsvenster, klembord-wistijd, tonen van wachtwoorden toestaan aan/uit.
- **Herstel:** herstelcodes opnieuw genereren, nood-account aan/uit en wachtwoord wijzigen.
- **Audit:** bewaartermijn, export, optioneel doorsturen via syslog.
- **Meldingen (optioneel):** melding bij inlog vanaf nieuw apparaat/IP.

## 13. Repository

- Keystone krijgt een **nieuwe, schone GitHub-repo `civr/keystone`** (privé, mijn keuze). `civr/Auth.i` blijft ongemoeid.
- `.superpowers/` staat in `.gitignore`.

## 14. Wat bij de builder ligt

- Exacte Go-libraries (SAML, OIDC, LDAP-server, WebAuthn), DB-schema, API-vorm.
- Mappenstructuur en monorepo-indeling.
- Testaanpak.
- Iconen: upload, of automatisch favicon ophalen (beide prima).
- Volgorde van bouwen. Voorstel: core + AD-login + passkey + startpagina eerst, daarna SAML (Bastion), OIDC (Nimbus), LDAP (Jellyfin), gate (Home Assistant), kluis (UniFi).
