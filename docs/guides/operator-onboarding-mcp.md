# Operator onboarding MCP for Claude and other AI clients

Remote URL:
`https://mcp.tesserix.app/mcp/onboarding/operator-onboarding-mcp`

Registry server: `devai/operator-onboarding-mcp`, tenant label `onboarding`.
The runtime belongs to
[`onboarding-service`](https://github.com/tesserix/onboarding-service), beside
the API it adapts. This repository supplies the authenticated Claude bridge
and a pinned copy of its generated contract. See
[ADR-0006](../adr/0006-operator-onboarding-mcp.md).

## Authorized tools

| Tool | Required additional role | Effect |
|---|---|---|
| `get_onboarding_context` | read | Current identity, permissions and instructions |
| `list_hospital_tenants` | read | Cursor-paginated active Helivanta entitlements |
| `get_hospital_onboarding` | read | Current status, generation and retained receipt |
| `request_hospital_onboarding` | read + create | Create a new hospital/group request |
| `adopt_hospital_onboarding` | read + adopt | Adopt a verified existing binding, preserving login policy |

Role names use the prefix `onboarding.helivanta.` in the AgentGateway project.
The default `onboarding-mcp-claude-prod` machine has read and create, plus the
route-specific `agentgateway.onboarding` role. It cannot reach other MCP or
model routes. It has no adoption role, Zitadel management role, Kubernetes
RBAC, clinical privileges, model quota grant or credential-management tool.
Tools the caller cannot use are absent from `tools/list` and cannot be invoked.
Create authority does not imply adopt authority.

These are privileged product onboarding tools. Do not attach them to a public
patient assistant or an ordinary hospital session. The hospital UUID is a
resource locator; the verified caller's identity and product authority always
come from the token. The initial implementation supports Helivanta identity
onboarding. Other product onboarding and clinical setup need their own reviewed
contract and signed roles; a model-supplied product argument cannot enable them.

## Connect Claude Code or Claude Desktop

1. Build the reviewed client from this repository:

   ```sh
   go build -o "$HOME/.local/bin/tesserix-onboarding-mcp" ./cmd/onboarding-mcp
   ```

2. Obtain the assigned client's credential through your authorized OpenBao
   reader. The platform's stored path is
   `kv/onboarding/prod/app/onboarding-mcp-claude-prod`, field `value`.
   Materialize it only into your private credential directory or a runtime
   secret mount with mode `0600`. For an already authorized Bao CLI session:

   ```sh
   umask 077
   mkdir -p "$HOME/.config/tesserix"
   bao kv get -field=value kv/onboarding/prod/app/onboarding-mcp-claude-prod \
     > "$HOME/.config/tesserix/onboarding-mcp-client-secret"
   ```

   Do not give Claude an OpenBao administrative token, kubeconfig or operator
   service-account token. The Kubernetes reader role is limited to
   `onboarding/onboarding-mcp-client`; it is a delivery identity, not an
   administrator account. External readers need an explicitly scoped OpenBao
   identity from the platform administrator. A denied read is not permission
   to use a broader recovery credential.

3. Register the stdio client in Claude Code:

   ```sh
   claude mcp add --transport stdio tesserix-onboarding -- \
     "$HOME/.local/bin/tesserix-onboarding-mcp" \
     --credential-file "$HOME/.config/tesserix/onboarding-mcp-client-secret"
   ```

   Claude Desktop's `mcpServers` entry uses the same absolute executable and
   arguments:

   ```json
   {
     "mcpServers": {
       "tesserix-onboarding": {
         "command": "/absolute/path/tesserix-onboarding-mcp",
         "args": ["--credential-file", "/private/path/client-secret"]
       }
     }
   }
   ```

4. Restart/reconnect the client and request `get_onboarding_context`. Verify
   the expected dedicated subject, product `helivanta`, `canCreate: true` and
   `canAdopt: false`. The bridge compares every discovered tool with its pinned
   contract and refuses unknown or changed tools. It obtains and refreshes
   OAuth access tokens in memory; no bearer token is saved in Claude's config.
   Restart it after rotating the client secret and updating the secret mount.

The remote endpoint also accepts standard MCP Streamable HTTP clients with a
fresh bearer token. This release uses assigned machine credentials. It does
not provide dynamic OAuth client registration or browser-based Claude web
connector login. The stdio bridge is the supported automatic-refresh path.
No client is automatically granted permission merely by discovering a server.

## Token contract for another MCP client

Use a distinct machine per agent and environment. Its Zitadel grant must be
explicitly declared in GitOps. Request `client_credentials` at
`https://auth.tesserix.app/oauth/v2/token` with HTTP Basic client authentication
and these scopes:

```text
openid
urn:zitadel:iam:org:project:id:387190457387450503:aud
urn:zitadel:iam:org:project:id:395476377970673709:aud
urn:zitadel:iam:org:projects:roles
```

The first project is Gateway; the second is the onboarding resource. Both
audiences are required. Tokens carry `agentgateway.onboarding` and explicit onboarding
roles under the project-scoped Gateway role claim, granted by the Tesserix
organization. Generic Gateway ADK tokens cannot use these tools. The MCP server
does not turn its caller into a console user or forward the token to Zitadel.
Respect `expires_in`; revoking a grant prevents new authorized tokens, while
existing JWTs remain usable until expiry. For emergency access revocation,
disable the MCP route/enablement through reviewed GitOps and remove the grant.

## Onboard a hospital safely

1. Ask the requesting operator whether this is a new independently administered
   hospital/group or a branch within an existing tenant. Confirm the display
   name and existing verified initial administrator's Zitadel user ID.
2. Use one organization for a hospital group only when its central identity
   administrators govern the hospitals, branches, pharmacies and labs. Model
   resource access explicitly in Helivanta. Independently administered entities
   need separate organizations. A shared staff member can retain one login
   with separate grants and product memberships in each authorized tenant.
3. Obtain explicit user intent for a production write. Record a stable request
   key in the authorized operational work record before calling:

   ```json
   {
     "displayName": "Example Hospital Group",
     "initialAdminUserId": "<verified-existing-user-id>",
     "idempotencyKey": "hospital-request-<stable-request-uuid>"
   }
   ```

4. Call `request_hospital_onboarding`. Save the returned `tenantId` and exact
   request key. The normal initial state is `pending`.
5. Call `get_hospital_onboarding` with `{"tenantId":"<returned-uuid>"}`.
   Completion requires `state: complete`, current observed generation,
   `IdentityReconciled`, activation, and non-empty archive object/generation.
   Use bounded polling and report a dependency failure honestly. On a timeout,
   replay the exact request/key rather than inventing another tenant.
6. Open `sourceLocator` in the console to review. Follow the
   [Helivanta runbook](https://github.com/tesserix/helivanta/blob/main/docs/delivery/hospital-onboarding.md)
   for clinical tenant configuration, branches, access policy, billing and
   broader operational readiness. Identity completion alone does not complete
   clinical onboarding or establish SOC2/HIPAA certification.

For existing customers such as YES Hospital, use status/read tools. Adoption
requires a separately granted `.adopt` role and confirmed existing tenant, org,
name and administrator identifiers; `preserveLoginPolicy` must be true. Never
create a replacement organization to work around missing adoption permission.

## Failure and recovery

- `401` means the bearer token, its audiences, expiry or exact role grants were
  rejected. Reauthenticate the assigned client; never switch to a shared admin.
- An absent write/adopt tool means that role was not granted. The gateway role
  alone is insufficient.
- `not_found` does not disclose whether a foreign resource exists.
- `conflict` means the same durable binding/key has different intent. Stop and
  inspect the retained request; do not retry with random new keys.
- `temporarily_unavailable` leaves the request resumable. Do not claim success.
- Contract mismatch requires updating the reviewed consumer pin with the
  published server contract. Do not bypass validation.

HTTP MCP is stateless: no session identifier or standalone SSE stream is
required, and a fresh process can resume using the retained tenant UUID.
Secrets remain in OpenBao and operator archives remain held/versioned in GCS.
