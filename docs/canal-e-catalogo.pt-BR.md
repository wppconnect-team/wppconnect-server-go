# Remoção de canal e alterações no catálogo

Use POST em `/api/{session}/{operação}` com o token da sessão, `Content-Type: application/json` e uma `Idempotency-Key` exclusiva para o comando. Na Cloud, o prefixo é `/api/v1/sessions/{session}/`. As escritas de catálogo sempre usam a identidade da sessão conectada.

| Operação | Corpo de exemplo ou campos | Confirmação |
| --- | --- | --- |
| `delete-newsletter` | `{"id":"123@newsletter"}` | 200 com o mesmo ID e estado `DELETED`. A exclusão é permanente. Exige cliente Web compatível e papel de dono. Administrador ou inscrito recebe 403. |
| `del-collection` | `{"collectionIds":["c1","c2"]}` | 200 com `acknowledged` e `requestedCount`. O protocolo confirma o lote. Para verificar cada coleção, percorra `own-catalog-collections` usando `next` como `after`. |
| `add-product` | `{"product":{"name":"Café","currency":"BRL","price":"12990","image_urls":["https://mmg.whatsapp.net/exemplo"]}}` | 201 com `response.product.id` e `reviewStatus`. O endereço de mídia é ilustrativo. A publicação para compradores depende da revisão do WhatsApp. |
| `edit-product` | `{"productId":"p1","product":{"name":"Café","is_hidden":false,"currency":"BRL","price":"14500","image_urls":["https://mmg.whatsapp.net/exemplo"]}}` | 200 após conferir o ID devolvido. Envie a ficha completa, incluindo a visibilidade pretendida. |
| `del-products` | `{"productIds":["p1","p2"]}` | 200 quando a contagem coincide. Em uma exclusão parcial, 409 com `status: partial`, `requestedCount` e `deletedCount`. Consulte cada ID com `own-catalog-product`. |

Os lotes aceitam de 1 a 100 IDs distintos. O catálogo aceita preço e preço promocional (`sale_price`) como strings inteiras em milésimos. Por exemplo, `"12990"` com `BRL` representa R$ 12,99. A moeda usa três letras maiúsculas. Informe de 1 a 10 referências HTTPS de imagens já hospedadas em domínios de mídia aceitos pelo SDK (WhatsApp ou Meta). O OpenAPI descreve os campos opcionais de descrição, referência do varejista, vídeo e conformidade. A criação assume visibilidade pública quando `is_hidden` é omitido.

Na Cloud, a exclusão de canal exige `channels:admin`, e as alterações de catálogo exigem `commerce:write`. Para consultar o estado atual do catálogo, use `commerce:read`.

Se perder a resposta, use GET em `newsletter-admin-result` (canal) ou `catalog-write-result` (catálogo), com a mesma sessão e chave. Repetir a mesma requisição com a mesma chave recupera o recibo. Reutilizar a chave com entrada diferente retorna 409. Uma resposta 502 com `status: unconfirmed` exige conferir o estado remoto antes de iniciar outro comando. A contagem parcial de produtos não identifica quais itens foram excluídos.

## Testes isolados

Execute `go test ./internal/httpapi -run 'TestOwnedNewsletterDeletion|TestOwnedCatalogProductContracts' -count=1`. A suíte usa clientes simulados, autenticação HTTP e um banco SQLite temporário. Verifica papel, identidade, confirmação perdida, repetição, acesso entre sessões, preços e exclusões parciais. O efeito em uma conta Business, a exclusão de um canal real e a comparação com a referência licenciada continuam sem aceite externo.
