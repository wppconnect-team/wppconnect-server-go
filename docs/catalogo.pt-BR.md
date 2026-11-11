# Catálogo da conta conectada

## Consultar produtos e coleções

Estas três operações do Server Go são de leitura. Use POST com JSON e o token da sessão em `/api/{session}/{operação}`. A conta vendedora é determinada no servidor pela identidade da sessão conectada. Não se aceita um `businessJID` para consultar outra empresa. Nenhuma destas rotas aceita `Idempotency-Key`.

| Operação | Corpo | Resposta dentro de `response` |
| --- | --- | --- |
| `own-catalog` | `{"limit":20,"after":"cursor anterior"}` | `products` e `next`. Limite de 1 a 50 produtos, padrão 20. |
| `own-catalog-product` | `{"productId":"123"}` | Um produto. Se o ID devolvido não for o solicitado, a resposta é 502. |
| `own-catalog-collections` | `{"limit":10}` | `collections` com ID e nome, mais `next`. Limite de 1 a 20 coleções, padrão 20. |

Envie `{}` na primeira página. Nas seguintes, copie o valor de `next` em `after`. O cursor é opaco e tem limite de 2048 bytes. Uma página vazia é válida; `next` vazio indica que o provedor não retornou outro cursor. Não há garantia de fotografia estável de um catálogo que muda entre páginas.

Produtos incluem `id`, `name`, `price`, `currency`, `hidden` e `availability`. Preço e moeda são as strings fornecidas pelo SDK, sem arredondamento ou conversão para a menor unidade monetária. Preserve os valores originais antes de formatá-los. Visibilidade falsa é um valor explícito. Imagens, tokens de mídia, endereços de importador e detalhes de moderação não fazem parte desta projeção.

A API rejeita campos desconhecidos e tamanhos fora dos limites com 400. Sem sessão conectada retorna 404; sem identidade telefônica da conta, 409. Falhas remotas não são tratadas como catálogo vazio. O contrato abrange parte de Z249, Z250, Z251 e Z269, com escopo restrito ao próprio vendedor. O teste com conta Business e catálogo real continua pendente.

Uma sessão pareada e conectada ainda pode receber uma recusa do provedor ao consultar o catálogo. Para uma falha GraphQL, a resposta inclui o código numérico que o WhatsApp forneceu, quando disponível. Um HTTP 502 com `GraphQL 400` indica que a consulta falhou. A quantidade de produtos e o estado do catálogo continuam desconhecidos. Para testar a escrita, confirme o catálogo no WhatsApp Business e use um produto destinado ao teste.

## Criar e atualizar coleções e alterar a visibilidade de produtos

Use POST na mesma sessão e inclua `Content-Type: application/json` e `Idempotency-Key`. Escolha uma chave por operação e guarde os bytes do corpo enviado. Na Cloud a credencial precisa do escopo `commerce:write`. O WhatsApp aplica as permissões da conta Business conectada.

| Operação | Exemplo de corpo | Confirmação |
| --- | --- | --- |
| `create-own-collection` | `{"name":"Verão","productIds":["10","20"]}` | 201 com `response.id` e `reviewStatus`. A revisão pode estar pendente. |
| `update-own-collection` | `{"collectionId":"42","name":"Ofertas","addProductIds":["30"],"removeProductIds":["10"]}` | 200 com o ID da coleção e o estado de revisão retornado. |
| `set-own-product-visibility` | `{"productId":"20","hidden":false}` | 200 com o ID e a visibilidade solicitada após confirmação do protocolo. |

A criação aceita de 1 a 100 IDs distintos. Na atualização, envie pelo menos uma alteração. As listas de inclusão e remoção admitem juntas até 100 IDs distintos. Nomes e IDs admitem até 256 bytes. Envie `hidden` como booleano explícito, inclusive para tornar o produto visível.

Consulte `GET /api/{session}/catalog-write-result` com a mesma chave e o mesmo token. A consulta recupera o código HTTP e o resultado original. Reutilizar a chave com outro corpo gera 409. Um erro 502 com `status: unconfirmed` indica que a confirmação remota se perdeu ou não pôde ser validada. Leia o produto ou a coleção e confira o estado antes de iniciar outra operação. A aprovação comercial e a visibilidade para compradores exigem validação com uma conta Business real.
