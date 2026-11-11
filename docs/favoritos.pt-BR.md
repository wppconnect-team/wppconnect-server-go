# Favoritos de mensagens

Envie `POST /api/{session}/star-message` com o token da sessão, `Idempotency-Key` e um corpo como `{"chatId":"123@s.whatsapp.net","messageId":"ID_OBSERVADO","star":false}`. O histórico da sessão precisa conter a mensagem do chat direto ou grupo informado. Para grupos, o servidor recupera o remetente a partir dessa mensagem. Envie `true` para favoritar.

HTTP 200 com `response.requested` significa que o SDK aceitou a alteração. Para ler o estado observado, envie os mesmos `chatId` e `messageId` a `POST message-star-state`. A resposta contém `starred` como booleano, ou `null` até que exista uma observação. O histórico preserva o evento depois do reinício enquanto retiver a mensagem. Uma confirmação incerta retorna 502. Recupere a resposta original com `GET message-state-result` e a mesma chave antes de emitir outro comando.
