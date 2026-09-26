# Roamie provider activation matrix

Tracking: https://github.com/tesserix/roamie/issues/96

These are provider candidates, not claims of official MCP availability. Wrap approved
provider APIs with the house runtime and register the resulting connector. Never
trust an unreviewed community MCP simply because its name matches a provider.

| Provider | Use | Current status / activation requirements |
| --- | --- | --- |
| Roamie API | Nearby places | Product MCP implemented; API workload authentication integration pending |
| Booking Demand | Stays | Read-only 3.1 adapter with sandbox default; affiliate contract/credentials and sandbox test pending |
| Klook | Activities | Explicit unavailable candidate; partner contract needed before live adapter |
| Amadeus | Flights and travel inventory | Evaluate [official developer APIs](https://developers.amadeus.com/); credentials, terms and schema pinning required |
| Viator | Experiences | Evaluate [Partner API](https://docs.viator.com/partner-api/technical/); partner access required |
| Google Maps | Places and routes/ETA | Roamie already has nearby places; [Routes API](https://developers.google.com/maps/documentation/routes) integration remains to implement |
| Reference FX | Currency benchmark | Existing daily FX is unsuitable for 15-minute real-time comparisons; approved fresh source needed |
| Exchange shops | Advertised executable rate | No connected feed; quotes need source, observation/expiry, pair, fees, amount bands and cash increment |
| Retailers | Shopping discounts | No connected feed; require attributable offers and expiry, then route enrichment |
| Image provider | Consented memories | No generation integration; require consent-bound asset IDs, private storage and provenance |

Amadeus, Viator and Google documentation pages were fetched successfully during
implementation. Frankfurter documentation could not be fetched in this environment;
no assumption is made about live freshness or availability. No external bookings,
purchases, partner registrations or production requests were performed.

Every connector needs an owner, independent package and image, a compiled manifest,
input/output schema digest, narrow scopes, tenant checks and gateway-only exposure.
Provider credentials stay server-side. Return unavailable on absent credentials;
placeholder keys are development configuration, never evidence of a live connection.
