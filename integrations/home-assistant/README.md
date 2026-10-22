# keystone_auth — automatisch inloggen in Home Assistant

Met deze component kom je via de Keystone-tegel direct binnen in Home Assistant, als de HA-gebruiker met
dezelfde gebruikersnaam als het *Account in de app* dat je in Keystone voor Home Assistant invult.

1. Zet Home Assistant achter je reverse proxy met `keystone-gate` (zie de hoofd-README, *Home Assistant*).
2. Kopieer `custom_components/keystone_auth` naar de `config/custom_components/` van Home Assistant.
3. Voeg toe aan `configuration.yaml`:

   ```yaml
   http:
     use_x_forwarded_for: true
     trusted_proxies:
       - 172.18.0.0/16        # je reverse proxy

   keystone_auth:
     trusted_proxies:
       - 172.18.0.0/16        # dezelfde proxy
   ```

4. Herstart Home Assistant.

Opent iemand de inlogpagina via de proxy met de header `X-Keystone-User`, dan meldt de component die
gebruiker meteen aan. Zonder header (of vanaf een ander adres) verschijnt het gewone inlogscherm, dus de
HA-app op je telefoon en lokale toegang blijven werken. De component vertrouwt de header alleen van de
adressen onder `trusted_proxies`; zorg dat je proxy `X-Keystone-*` van bezoekers nooit doorgeeft.

Tests van de hulpfuncties: `uv run --with pytest python -m pytest integrations/home-assistant/tests`.
