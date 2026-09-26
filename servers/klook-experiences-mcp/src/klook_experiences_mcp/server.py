from collections.abc import Mapping
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field
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


class Search(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)
    destination: str = Field(min_length=1, max_length=200)
    interests: Annotated[
        list[Annotated[str, Field(min_length=1, max_length=80)]], Field(max_length=12)
    ]


class Result(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)
    status: Literal["unavailable"] = Field(default="unavailable", max_length=20)
    reason: Literal["partner_contract_required"] = Field(
        default="partner_contract_required", max_length=40
    )


async def search_activities(criteria: Search) -> Result:
    return Result()


def catalog() -> ToolCatalog:
    return ToolCatalog(
        [
            callable_tool(
                search_activities,
                metadata=ToolMetadata(
                    name="search_activities",
                    title="Klook experiences candidate",
                    description=(
                        "Unavailable until the Klook partner contract "
                        "and credentials are configured."
                    ),
                    effect=ToolEffect.READ,
                    approval=ApprovalRequirement.NOT_REQUIRED,
                    idempotency=IdempotencyRequirement.NOT_APPLICABLE,
                    required_scopes=("travel:activities:read",),
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


def application(*, transport: ApplicationTransport) -> Application:
    return Application(
        catalog=catalog(),
        authorizer=Authorization(),
        transport=transport,
        telemetry=Telemetry(),
        limits=ApplicationLimits(drain_timeout=10),
        clock=SystemClock(),
        execution_limits=ExecutionLimits(max_call_seconds=2, max_tool_seconds=1, max_attempts=1),
    )
