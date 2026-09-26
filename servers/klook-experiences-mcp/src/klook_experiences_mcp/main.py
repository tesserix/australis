import asyncio
import hmac
import os
import signal

from tesserix_mcp_runtime import CallContext, Cancellation, SecretRedactor, SecretValue
from tesserix_mcp_runtime.adapters.gateway_identity import (
    GatewayIdentityConfig,
    GatewayJWTContextProvider,
)
from tesserix_mcp_runtime.adapters.streamable_http import (
    HTTPRequestAuthenticationError,
    HTTPRequestMetadata,
    StreamableHTTPConfig,
    StreamableHTTPLimits,
    StreamableHTTPTransport,
)

from klook_experiences_mcp.server import Telemetry, application


class ContextProvider:
    def __init__(self, verifier: GatewayJWTContextProvider, key: str) -> None:
        if len(key) < 32:
            raise ValueError("MCP key must contain at least 32 characters")
        self._verifier = verifier
        self._key = key

    async def create(
        self, request: HTTPRequestMetadata, *, cancellation: Cancellation
    ) -> CallContext:
        values = request.header_values("X-MCP-Key")
        if len(values) != 1 or not hmac.compare_digest(values[0], self._key):
            raise HTTPRequestAuthenticationError(request_id="rejected")
        return await self._verifier.create(request, cancellation=cancellation)


async def serve() -> None:
    upstream_key = os.environ["KLOOK_MCP_KEY"]
    partner_token = os.environ.get("KLOOK_PARTNER_TOKEN", "")
    verifier = GatewayJWTContextProvider(
        GatewayIdentityConfig(
            issuer=os.environ["KLOOK_MCP_ISSUER"],
            audience=os.environ["KLOOK_MCP_AUDIENCE"],
            jwks_url=os.environ["KLOOK_MCP_JWKS_URL"],
            jwks_allowed_hosts=tuple(os.environ["KLOOK_MCP_JWKS_HOSTS"].split(",")),
            trusted_proxy_cidrs=tuple(os.environ["KLOOK_MCP_GATEWAY_CIDRS"].split(",")),
        )
    )
    transport = StreamableHTTPTransport(
        config=StreamableHTTPConfig(
            host=os.environ.get("KLOOK_MCP_HOST", "127.0.0.1"),
            port=8080,
            allowed_hosts=tuple(os.environ["KLOOK_MCP_ALLOWED_HOSTS"].split(",")),
            allowed_origins=tuple(os.environ["KLOOK_MCP_ALLOWED_ORIGINS"].split(",")),
        ),
        limits=StreamableHTTPLimits(max_tools=1, tool_page_size=1, max_tool_pages=1),
        context_provider=ContextProvider(verifier, upstream_key),
        telemetry=Telemetry(),
        redactor=SecretRedactor(
            known_secrets=tuple(
                SecretValue(value) for value in (upstream_key, partner_token) if value
            )
        ),
    )
    app = application(transport=transport)
    stop = asyncio.Event()
    for received in (signal.SIGINT, signal.SIGTERM):
        asyncio.get_running_loop().add_signal_handler(received, stop.set)
    await app.start()
    try:
        await stop.wait()
    finally:
        await app.drain()
        await app.stop()


if __name__ == "__main__":
    asyncio.run(serve())
