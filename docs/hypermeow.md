# HyperMeow no server-go

O servidor usa o SDK publico [wppconnect-team/hypermeow](https://github.com/wppconnect-team/hypermeow), derivado de polymorfa/hypermeow e tulir/whatsmeow. O namespace de imports continua `github.com/polymorfa/hypermeow`; o `replace` versionado aponta ao fork WPPConnect. Nao use um replace de `go.mau.fi/whatsmeow` para esse SDK: os tipos protobuf e imports internos precisam compartilhar o namespace.

A revisao fixada no go.mod inclui os protocolos tulir ate c88388f e a correcao da migracao SQLite da base anterior. Nenhuma dependencia aponta para um caminho local. As licencas MPL-2.0 e MIT estao preservadas nos notices do artefato.

## Resolucao de destinatarios

A consulta de perfil pode omitir o LID de um numero registrado. O SDK consulta o contato nesse caso e aceita somente um resultado registrado cujo telefone consultado corresponda ao pedido original. Preserva o identificador canonico informado pelo WhatsApp, sem remover ou adicionar digitos por heuristica. Essa correcao esta incorporada no HyperMeow PR #4 e evita rejeitar um destinatario valido antes da transmissao.

Um HTTP 502 de envio continua sendo tratado como resultado potencialmente incerto pelo ledger. Nao repita o comando com outra chave automaticamente: consulte `send-result` e investigue o resultado original. Uma correcao de resolucao nao altera retroativamente os registros de idempotencia.

## Migracao de sessoes

O Whatsmeow anterior usa schema 16; o HyperMeow usa schema 19. O mesmo numero 16 tem significados diferentes nos forks. O upgrade verifica a presenca de `wasa_root_secret_id`, preserva seu valor e cria o indice de sender keys que pode faltar na origem. A coluna `username` e adicionada pela migracao 18.

O teste `TestHyperMeowUpgradesPreviousRuntimeDatabaseWithoutLosingCredentials` parte do SQL exato do fork anterior 800567d5cf16, com uma identidade de teste. Confere chaves Noise/identidade, segredo ADV, segredo WASA existente, indice, versao e repeticao do upgrade. Nao usa dados reais de clientes.

Antes de atualizar uma instancia existente, pare o escritor e faca backup consistente de todo o DATA_DIR, incluindo bancos de sessao, historico e outbox. Teste a restauracao em uma copia isolada. Nao execute duas versoes sobre o mesmo banco. Para rollback, restaure o backup e a imagem anterior juntos; nao reduza manualmente o numero do schema e nao prometa downgrade no mesmo arquivo.

## Escopo funcional

A adocao do SDK preserva as rotas HTTP atuais; nao cria automaticamente endpoints para todos os recursos publicos do HyperMeow. Recursos de comercio, flows e chamadas precisam de adaptadores, contratos e testes proprios. O projeto publico nao contem o fonte completo do aplicativo Zuck nem do seu replace `hypermeow-private`. A imagem de referencia e uma evidencia de contrato e comportamento, nao uma implementacao-fonte recuperada.

Testes offline aprovados nao comprovam pareamento, envio ou recebimento real. O rollout do novo SDK exige canary com volume isolado, QR, reconexao e jornadas autorizadas de mensagens antes de migrar sessoes conectadas. A pool Go-only e independente dessa troca de SDK.
