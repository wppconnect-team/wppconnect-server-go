# Receber eventos no n8n

Importe [receive-events.json](receive-events.json), escolha uma credencial do tipo **Header Auth** e publique o workflow. Use `X-WPP-Workflow-Token` como nome do header e um segredo aleatório de pelo menos 32 caracteres. Guarde o mesmo valor em `N8N_WEBHOOK_TOKEN` no runtime. O arquivo de exemplo não contém a credencial.

Defina `N8N_WEBHOOK_URL` como a URL de produção HTTPS terminada em `/webhook/wppconnect-events`. O caminho temporário `/webhook-test/` não é aceito. HTTP sem TLS só é permitido em endereço IP de loopback para testes locais. O destino é opcional e configurado pelo operador; com URL vazia não há envio ao n8n. Não use um destino compartilhado com clientes que não podem acessar os mesmos eventos. O exemplo desativa a persistência de dados das execuções para evitar guardar headers e mensagens na instância de teste. Revise retenção e logs de proxies antes de usar dados reais.

O runtime envia o JSON do evento no corpo e a identidade estável em `X-WPP-Event-ID`. O workflow de exemplo retorna apenas `eventId`, `event` e `session`, sem repassar a credencial. Ele não envia mensagens a ninguém nem cria recursos em outros serviços. Antes de adicionar um efeito externo, implemente deduplicação transacional pelo ID do evento: uma resposta perdida pode causar nova entrega. O exemplo não implementa essa deduplicação para o seu sistema.

O destino `n8n` é independente de `webhook`: pode falhar e ser reprocessado sem repetir entregas que outro destino concluiu. Use a [consulta e o replay de entregas](../../docs/event-delivery-operations.md) com `target=n8n`. O aceite HTTP confirma somente a resposta do workflow; o efeito comercial de uma etapa posterior precisa de verificação própria.

Para validar em ambiente Linux com Docker e Go, execute `bash scripts/test-n8n.sh` na raiz do repositório. O teste importa o workflow e uma credencial temporária em um n8n isolado, rejeita uma credencial errada e verifica a resposta de uma execução autenticada.
