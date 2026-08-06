# Synthetic provider contract fixtures

The JSON files in this directory are handwritten, deterministic fixtures shaped
from the public Route Details and HTTP Geocoder response documentation. They are
not captured Yandex responses and contain no provider or user data.

Official schema references (checked 2026-08-05):

- https://yandex.ru/maps-api/docs/router-api/response.html
- https://yandex.ru/maps-api/docs/router-api/request.html
- https://yandex.com/maps-api/docs/geocoder-api/response.html
- https://yandex.com/maps-api/docs/geocoder-api/request.html

The executable contract assertions live in
`services/provider-yandex/*_contract_test.go` so they can exercise the unexported
wire-to-domain normalization boundary without exposing provider DTOs.

The `dgis-*` fixtures are likewise handwritten from the public Routing 7.0.0
and Geocoder 3.0 schemas. They contain no captured response, subscription key,
or user coordinates. Official references:

- https://docs.2gis.com/en/api/navigation/routing/reference/routing
- https://docs.2gis.com/api/search/geocoder/reference/3.0/items/geocode
