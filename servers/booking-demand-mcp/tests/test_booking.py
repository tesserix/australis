import secrets

import httpx

from booking_demand_mcp.booking import BookingClient, Search


async def test_partner_search_preserves_display_price_and_total_separately():
    def reply(request):
        assert request.url.path == "/3.1/accommodations/search"
        assert request.headers["X-Affiliate-Id"] == "123"
        return httpx.Response(
            200,
            json={
                "data": [{"id": 12, "currency": "AUD", "price": {"book": 100.25, "total": 110.25}}]
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(reply)) as http:
        client = BookingClient(
            http, affiliate_id="123", token=secrets.token_urlsafe(32), enabled=True
        )
        result = await client.search(
            Search(
                checkin="2026-10-01", checkout="2026-10-03", country="au", city=1, currency="AUD"
            )
        )
        assert result.status == "ok"
        assert result.stays[0].display_price == "100.25"
        assert result.stays[0].total_price == "110.25"
        assert result.stays[0].property_id == 12


async def test_disabled_provider_does_not_send_example_credentials():
    def no_network(request):
        raise AssertionError("disabled provider must not make a request")

    async with httpx.AsyncClient(transport=httpx.MockTransport(no_network)) as http:
        result = await BookingClient(
            http, affiliate_id="123", token=secrets.token_urlsafe(32)
        ).search(
            Search(
                checkin="2026-10-01", checkout="2026-10-03", country="au", city=1, currency="AUD"
            )
        )
        assert result.status == "unavailable"
        assert result.stays == []


async def test_booking_tool_runs_inside_tesserix_runtime():
    from tesserix_mcp_runtime import AuthenticatedIdentity, CallContext
    from tesserix_mcp_runtime.adapters.in_process import InProcessTransport

    from booking_demand_mcp.server import application

    async with httpx.AsyncClient() as http:
        transport = InProcessTransport()
        app = application(
            transport=transport, client=BookingClient(http, affiliate_id="", token="")
        )
        await app.start()
        try:
            result = await transport.invoke(
                "search_stays",
                {
                    "criteria": {
                        "checkin": "2026-10-01",
                        "checkout": "2026-10-03",
                        "country": "au",
                        "city": 1,
                        "currency": "AUD",
                    }
                },
                context=CallContext(
                    identity=AuthenticatedIdentity(
                        tenant="roamie",
                        subject="user",
                        issuer="https://identity.example.org",
                        scopes=("travel:stays:read",),
                    ),
                    request_id="request",
                    run_id="run",
                ),
            )
            assert result.error is None
            assert result.value == {"status": "unavailable", "stays": []}
        finally:
            await app.drain()
            await app.stop()
