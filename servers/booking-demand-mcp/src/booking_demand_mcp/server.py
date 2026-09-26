from collections.abc import Mapping
from typing import Any

from tesserix_mcp_runtime import (
    Application,
    ApplicationLimits,
    ApplicationTransport,
    ApprovalRequirement,
    CallContext,
    ErrorCode,
    ExecutionLimits,
    IdempotencyRequirement,
    JsonValue,
    RuntimeFailure,
    SystemClock,
    ToolCatalog,
    ToolDefinition,
    ToolEffect,
    ToolMetadata,
)
from tesserix_mcp_runtime.adapters.mcp_authoring import callable_tool

from booking_demand_mcp.booking import BookingClient, Search, SearchResult


def catalog(client: BookingClient) -> ToolCatalog:
    async def search_stays(criteria: Search) -> SearchResult:
        return await client.search(criteria)

    return ToolCatalog(
        [
            callable_tool(
                search_stays,
                metadata=ToolMetadata(
                    name="search_stays",
                    title="Search Booking.com stays",
                    description="Read accommodation offers with separate display and total prices.",
                    effect=ToolEffect.READ,
                    approval=ApprovalRequirement.NOT_REQUIRED,
                    idempotency=IdempotencyRequirement.NOT_APPLICABLE,
                    required_scopes=("travel:stays:read",),
                ),
            )
        ]
    )


class Authorization:
    async def authorize(
        self,
        *,
        tool: ToolDefinition[Any, Any],
        arguments: Mapping[str, JsonValue],
        context: CallContext,
    ) -> None:
        if context.tenant != "roamie" or not set(tool.metadata.required_scopes) <= set(
            context.scopes
        ):
            raise RuntimeFailure(ErrorCode.FORBIDDEN)


class Telemetry:
    def emit(self, event: object) -> None:
        return None


def application(*, transport: ApplicationTransport, client: BookingClient) -> Application:
    return Application(
        catalog=catalog(client),
        authorizer=Authorization(),
        transport=transport,
        telemetry=Telemetry(),
        limits=ApplicationLimits(drain_timeout=10),
        clock=SystemClock(),
        execution_limits=ExecutionLimits(max_call_seconds=10, max_tool_seconds=9, max_attempts=1),
    )
