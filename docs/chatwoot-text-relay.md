# Texto entre uma sessão Cloud e uma conversa do Chatwoot

Compile com `go build ./cmd/chatwoot-text-relay` e execute o binário com a configuração abaixo. O processo escuta em `127.0.0.1:8098/chatwoot`. No Chatwoot, assine o evento `message_created` e indique a URL HTTPS do proxy que encaminha a requisição para esse endereço, preservando o corpo original e os cabeçalhos de assinatura.

| Variáveis de ambiente | Valor esperado |
| --- | --- |
| `CHATWOOT_ACCOUNT_ID`, `CHATWOOT_INBOX_ID` | IDs numéricos da conta e da inbox de API dedicada. |
| `CHATWOOT_CONVERSATION_ID`, `CHATWOOT_CONTACT_ID` | Número da conversa exibido na inbox e ID do contato autorizado para configuração manual. |
| `CHATWOOT_WEBHOOK_SECRET` | Segredo de assinatura do webhook gerado pelo Chatwoot (24 caracteres por padrão). |
| `WPP_CLOUD_API_KEY`, `WPP_SESSION_ID` | Chave Cloud com escopo `messages:send` e ID da sessão pareada. |
| `WPP_PHONE` | Número autorizado com código do país e apenas dígitos. |
| `RELAY_STATE_DIR` | Caminho absoluto de um diretório privado em disco persistente. |

Para provisionar o contato e a conversa na inicialização, defina `CHATWOOT_PROVISION=true` e omita `CHATWOOT_CONTACT_ID` e `CHATWOOT_CONVERSATION_ID`. Configure também as variáveis do sentido WhatsApp para Chatwoot descritas abaixo. O adaptador usa o telefone definido pelo operador, a conta e a inbox de API. A chave do Chatwoot precisa de acesso a contatos, à inbox e às conversas. Use um diretório persistente dedicado a essa rota.

O adaptador consulta o telefone completo no Chatwoot. Reutiliza um único contato com número idêntico, incluindo o DDI, e preserva seus dados. Quando necessário, cria o contato ou o vínculo com a inbox. Em seguida cria uma conversa identificada pelo marcador da rota. Um contato bloqueado, múltiplas correspondências ou uma resposta de busca incompleta interrompem a inicialização. O modo manual permite usar uma conversa existente indicada pelo operador.

O arquivo `provision.db` reserva cada criação antes do POST e registra os IDs confirmados. Em caso de falha, reinicie com o mesmo diretório e a mesma rota. O adaptador consulta o estado no Chatwoot antes de prosseguir. Se a operação ainda estiver indefinida, ele mantém a reserva e informa qual etapa precisa de conferência. A busca de contatos cobre até dez páginas, e a reconciliação de uma criação de conversa sem resposta usa a janela dos 25 registros que o Chatwoot oferece. Depois de confirmado o ID, a consulta de reinício é direta. Preserve `provision.db` junto com `receipts.db` no backup.

Use um único banco de recibos para o vínculo. Ele guarda até 100.000 IDs. Ao atingir o limite, o serviço devolve 503 para novas mensagens e preserva as confirmações anteriores. Um recibo pendente após queda exige reconciliação com o resultado registrado na Cloud.

O comando `cmd/chatwoot-text-relay` liga uma única conversa preexistente a um único destinatário autorizado na Cloud. Os testes usam SQLite e um serviço HTTP local que representa a Cloud. O operador inicia o adaptador como serviço separado.

Só o evento `message_created` de texto público enviado por um atendente humano é elegível. Conta, inbox, conversa e contato devem coincidir com a configuração do operador. O destinatário vem exclusivamente da configuração. Notas privadas, anexos, ecos, outros contatos e mensagens de bots são ignorados. O teste de autenticação verifica a assinatura sobre os bytes originais e a idade do timestamp.

O banco grava o ID da mensagem antes da primeira tentativa de envio. Se a resposta se perder, a repetição do callback pode apenas consultar a chave da operação original. Uma consulta sem resultado, pendente ou ambígua não autoriza novo envio. A confirmação exige o resultado concluído com HTTP 201 e um ID de mensagem. Isso não comprova entrega nem leitura no telefone.

Para reproduzir os testes sem credenciais ou rede externa, execute `go test -v ./cmd/chatwoot-text-relay`. Os cenários cobrem reinício, mudança de vínculo, concorrência, assinatura inválida, relógio fora da janela, supressão de notas e confirmação perdida. O CI também cria uma inbox de API temporária na versão 4.19.1 do Chatwoot, usando PostgreSQL e Redis. A Cloud de envio de texto é simulada nesse teste.

O formato de entrada foi conferido no [produtor da mensagem](https://github.com/chatwoot/chatwoot/blob/af2a870c6d51aecf2a76388cf7b181dcf934c0fa/app/models/message.rb) e no [formato da conversa](https://github.com/chatwoot/chatwoot/blob/af2a870c6d51aecf2a76388cf7b181dcf934c0fa/app/presenters/conversations/event_data_presenter.rb). O Chatwoot documenta a [verificação da assinatura](https://www.chatwoot.com/hc/user-guide/articles/1677693021-how-to-use-webhooks).

## Texto recebido do WhatsApp

Para habilitar o sentido WhatsApp para Chatwoot, acrescente estas variáveis ao mesmo processo e configure o webhook assinado da sessão para a URL HTTPS do proxy que encaminha `POST /wpp`. A URL do webhook da sessão Cloud é definida na criação. Use a imagem Go que fornece o campo `persistentText` nos eventos de mensagem.

| Variável | Configuração |
| --- | --- |
| `CHATWOOT_BASE_URL` | Origem HTTPS da sua instalação, por exemplo `https://atendimento.example.org`. |
| `CHATWOOT_API_TOKEN` | Token de acesso de um usuário com acesso à conta e à inbox configuradas. |
| `WPP_WEBHOOK_SECRET` | Mesmo segredo de pelo menos 32 bytes usado na assinatura do webhook da sessão Go. |
| `WPP_PEER_LID` | Opcional. Identificador WhatsApp alternativo confirmado para o mesmo contato, no formato numérico seguido de `@lid`. |

A elegibilidade `persistentText` pertence ao evento observado ao vivo. O servidor examina também o indicador de visualização única dentro de texto estendido. Consultas ao histórico podem conter envelopes já normalizados e omitem essa indicação, inclusive ao ler bancos de versões anteriores. O adaptador usa o webhook assinado para decidir se encaminha o texto.

O adaptador verifica a assinatura WPP, a sessão, o remetente e o tipo da mensagem. Encaminha texto direto recebido cujo envelope indica texto comum. Grupos, texto temporário, anexos, ecos, eventos antigos sem a marca de elegibilidade e outras conversas são ignorados. Antes de gravar, consulta a conversa pela API do Chatwoot e confere conta, inbox de API e contato atuais. Use uma inbox dedicada ao vínculo.

A coluna `source_id` da mensagem no Chatwoot recebe um identificador estável derivado da sessão e do ID de mensagem WhatsApp. Depois de uma confirmação perdida, o adaptador consulta a página mais recente de mensagens. Se a mensagem saiu dessa página, a operação permanece pendente para conferência. Uma nova entrega pode completar o recibo quando a mensagem correspondente estiver visível. Preserve o banco local durante a recuperação.

O teste de inbox cria um contato e uma conversa descartáveis, processa um callback de atendente assinado pelo próprio Chatwoot e confere pela API que duas entregas sintéticas do mesmo texto do WhatsApp resultam em uma mensagem recebida. O teste de provisionamento interrompe a resposta depois da gravação no Chatwoot e confere os IDs em uma nova inicialização. Transferência de anexos e entrega em dispositivos continuam como etapas de aceite.
