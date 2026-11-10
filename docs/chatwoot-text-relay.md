# Respostas públicas do Chatwoot para uma sessão Cloud

Compile com `go build ./cmd/chatwoot-text-relay` e execute o binário com a configuração abaixo. O processo escuta em `127.0.0.1:8098/chatwoot`. No Chatwoot, assine o evento `message_created` e indique a URL HTTPS do proxy que encaminha a requisição para esse endereço, preservando o corpo original e os cabeçalhos de assinatura.

| Variáveis de ambiente | Valor esperado |
| --- | --- |
| `CHATWOOT_ACCOUNT_ID`, `CHATWOOT_INBOX_ID` | IDs numéricos da conta e da inbox de API dedicada. |
| `CHATWOOT_CONVERSATION_ID`, `CHATWOOT_CONTACT_ID` | Número da conversa exibido na inbox e ID do contato autorizado. |
| `CHATWOOT_WEBHOOK_SECRET` | Segredo de assinatura do webhook gerado pelo Chatwoot (24 caracteres por padrão). |
| `WPP_CLOUD_API_KEY`, `WPP_SESSION_ID` | Chave Cloud com escopo `messages:send` e ID da sessão pareada. |
| `WPP_PHONE` | Número autorizado com código do país e apenas dígitos. |
| `RELAY_STATE_DIR` | Caminho absoluto de um diretório privado em disco persistente. |

Use um único banco de recibos para o vínculo. Ele guarda até 100.000 IDs. Ao atingir o limite, o serviço devolve 503 para novas mensagens e preserva as confirmações anteriores. Um recibo pendente após queda exige reconciliação com o resultado registrado na Cloud.

O comando `cmd/chatwoot-text-relay` liga uma única conversa preexistente a um único destinatário autorizado na Cloud. Os testes usam SQLite e um serviço HTTP local que representa a Cloud. Nenhuma conta ou inbox real foi configurada. O piloto não inicia com o server-go.

Só o evento `message_created` de texto público enviado por um atendente humano é elegível. Conta, inbox, conversa e contato devem coincidir com a configuração do operador. O destinatário vem exclusivamente da configuração. Notas privadas, anexos, ecos, outros contatos e mensagens de bots são ignorados. O teste de autenticação verifica a assinatura sobre os bytes originais e a idade do timestamp.

O banco grava o ID da mensagem antes da primeira tentativa de envio. Se a resposta se perder, a repetição do callback pode apenas consultar a chave da operação original. Uma consulta sem resultado, pendente ou ambígua não autoriza novo envio. A confirmação exige o resultado concluído com HTTP 201 e um ID de mensagem. Isso não comprova entrega nem leitura no telefone.

Para reproduzir os testes sem credenciais ou rede externa, execute `go test -v ./cmd/chatwoot-text-relay`. Os cenários cobrem reinício, mudança de vínculo, concorrência, assinatura inválida, relógio fora da janela, supressão de notas e confirmação perdida. A jornada inversa, sincronização de contatos, anexos e aceite com Chatwoot real continuam em aberto.

O formato de entrada foi conferido no [produtor da mensagem](https://github.com/chatwoot/chatwoot/blob/af2a870c6d51aecf2a76388cf7b181dcf934c0fa/app/models/message.rb) e no [formato da conversa](https://github.com/chatwoot/chatwoot/blob/af2a870c6d51aecf2a76388cf7b181dcf934c0fa/app/presenters/conversations/event_data_presenter.rb). O Chatwoot documenta a [verificação da assinatura](https://www.chatwoot.com/hc/user-guide/articles/1677693021-how-to-use-webhooks).
