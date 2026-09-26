import json
from datetime import UTC, date, datetime
from decimal import Decimal
from typing import Annotated, Literal, Self

import httpx
from pydantic import BaseModel, ConfigDict, Field, model_validator


class Contract(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid")


class Search(Contract):
    checkin: str = Field(min_length=10, max_length=10)
    checkout: str = Field(min_length=10, max_length=10)
    country: str = Field(min_length=2, max_length=2)
    city: int = Field(ge=-10000000, le=10000000, strict=True)
    currency: str = Field(min_length=3, max_length=3)
    adults: int = Field(default=1, ge=1, le=10, strict=True)
    rooms: int = Field(default=1, ge=1, le=10, strict=True)
    children: Annotated[
        list[Annotated[int, Field(ge=0, le=17, strict=True)]], Field(max_length=10)
    ] = Field(default_factory=list)
    platform: Literal["android", "ios", "desktop", "mobile", "tablet"] = "mobile"

    @model_validator(mode="after")
    def valid_dates(self) -> Self:
        if (
            not 1
            <= (date.fromisoformat(self.checkout) - date.fromisoformat(self.checkin)).days
            <= 90
        ):
            raise ValueError("stay must be between one and ninety nights")
        if not self.country.isalpha() or not self.country.islower():
            raise ValueError("country must be lowercase ISO country code")
        if not self.currency.isalpha() or not self.currency.isupper():
            raise ValueError("currency must be uppercase ISO currency code")
        return self


class Stay(Contract):
    property_id: int = Field(ge=1, le=10**12, strict=True)
    currency: str = Field(min_length=3, max_length=3)
    display_price: str = Field(min_length=1, max_length=40)
    total_price: str | None = Field(default=None, max_length=40)
    observed_at: str = Field(min_length=1, max_length=40)
    source_url: str = Field(min_length=1, max_length=2048)


class SearchResult(Contract):
    status: Literal["ok", "unavailable"]
    stays: Annotated[list[Stay], Field(max_length=20)] = Field(default_factory=list)


def price(value: object) -> str:
    if not isinstance(value, Decimal | int | str) or isinstance(value, bool):
        raise ValueError("invalid price")
    number = Decimal(value)
    if not number.is_finite() or not 0 < number < Decimal("1000000000000"):
        raise ValueError("invalid price")
    return format(number, "f")


class BookingClient:
    def __init__(
        self,
        http: httpx.AsyncClient,
        *,
        affiliate_id: str,
        token: str,
        enabled: bool = False,
        sandbox: bool = True,
    ) -> None:
        self._http = http
        self._affiliate_id = affiliate_id
        self._token = token
        self._enabled = enabled
        self._origin = (
            "https://demandapi-sandbox.booking.com" if sandbox else "https://demandapi.booking.com"
        )

    async def search(self, criteria: Search) -> SearchResult:
        if not self._enabled or not self._affiliate_id.isdigit() or not self._token:
            return SearchResult(status="unavailable")
        payload = {
            "booker": {"country": criteria.country, "platform": criteria.platform},
            "checkin": criteria.checkin,
            "checkout": criteria.checkout,
            "city": criteria.city,
            "currency": criteria.currency,
            "guests": {
                "number_of_adults": criteria.adults,
                "number_of_rooms": criteria.rooms,
                "children": criteria.children,
            },
            "rows": 20,
        }
        try:
            async with self._http.stream(
                "POST",
                self._origin + "/3.1/accommodations/search",
                json=payload,
                headers={
                    "Authorization": f"Bearer {self._token}",
                    "X-Affiliate-Id": self._affiliate_id,
                },
                timeout=8,
                follow_redirects=False,
            ) as response:
                response.raise_for_status()
                raw = bytearray()
                async for part in response.aiter_bytes():
                    raw.extend(part)
                    if len(raw) > 131072:
                        return SearchResult(status="unavailable")
            body = json.loads(raw, parse_float=Decimal)
            data = body["data"]
            if not isinstance(data, list) or len(data) > 20:
                return SearchResult(status="unavailable")
            observed = datetime.now(UTC).isoformat()
            stays = []
            for item in data:
                if item["currency"] != criteria.currency:
                    raise ValueError("currency mismatch")
                stays.append(
                    Stay(
                        property_id=item["id"],
                        currency=item["currency"],
                        display_price=price(item["price"]["book"]),
                        total_price=price(item["price"]["total"])
                        if item["price"].get("total")
                        else None,
                        observed_at=observed,
                        source_url=f"{self._origin}/3.1/accommodations/search#accommodation={item['id']}",
                    )
                )
            return SearchResult(status="ok", stays=stays)
        except httpx.HTTPError, ValueError, KeyError, TypeError, ArithmeticError:
            return SearchResult(status="unavailable")
