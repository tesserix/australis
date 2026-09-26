from tesserix_mcp_runtime import AuthenticatedIdentity, CallContext
from tesserix_mcp_runtime.adapters.in_process import InProcessTransport

from klook_experiences_mcp.server import application


async def test_no_partner_contract_cannot_return_fabricated_inventory():
    transport = InProcessTransport()
    app = application(transport=transport)
    await app.start()
    try:
        result = await transport.invoke(
            "search_activities",
            {
                "criteria": {
                    "destination": "Melbourne",
                    "interests": ["museums"],
                }
            },
            context=CallContext(
                identity=AuthenticatedIdentity(
                    tenant="roamie",
                    subject="user",
                    issuer="https://identity.example.org",
                    scopes=("travel:activities:read",),
                ),
                request_id="request",
                run_id="run",
            ),
        )
        assert result.error is None
        assert result.value == {"status": "unavailable", "reason": "partner_contract_required"}
    finally:
        await app.drain()
        await app.stop()
