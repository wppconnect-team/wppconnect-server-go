# Consultar o catálogo da conta conectada

Estas três operações do Server Go são de leitura. Use POST com JSON e o token da sessão em `/api/{session}/{operação}`. A conta vendedora é determinada no servidor pela identidade da sessão conectada. Não se aceita um `businessJID` para consultar outra empresa. Nenhuma destas rotas aceita `Idempotency-Key`.

| Operação | Corpo | Resposta dentro de `response` |
| --- | --- | --- |
| `own-catalog` | `{"limit":20,"after":"cursor anterior"}` | `products` e `next`. Limite de 1 a 50 produtos, padrão 20. |
| `own-catalog-product` | `{"productId":"123"}` | Um produto. Se o ID devolvido não for o solicitado, a resposta é 502. |
| `own-catalog-collections` | `{"limit":10}` | `collections` com ID e nome, mais `next`. Limite de 1 a 20 coleções, padrão 20. |

Envie `{}` na primeira página. Nas seguintes, copie o valor de `next` em `after`. O cursor é opaco e tem limite de 2048 bytes. Uma página vazia é válida; `next` vazio indica que o provedor não retornou outro cursor. Não há garantia de fotografia estável de um catálogo que muda entre páginas.

Produtos incluem `id`, `name`, `price`, `currency`, `hidden` e `availability`. Preço e moeda são as strings fornecidas pelo SDK, sem arredondamento ou conversão para a menor unidade monetária. Preserve os valores originais antes de formatá-los. Visibilidade falsa é um valor explícito. Imagens, tokens de mídia, endereços de importador e detalhes de moderação não fazem parte desta projeção.

A API rejeita campos desconhecidos e tamanhos fora dos limites com 400. Sem sessão conectada retorna 404; sem identidade telefônica da conta, 409. Falhas remotas não são tratadas como catálogo vazio. O contrato abrange parte de Z249, Z250, Z251 e Z269, com escopo restrito ao próprio vendedor. Não cria produtos, coleções, pedidos nem pagamentos. O teste com conta Business e catálogo real continua pendente.
