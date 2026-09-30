import 'dart:convert';

import 'client.dart';
import 'types.dart';

/// Client for the ResolveSpec JSON body protocol: POST {operation, data, options}.
///
/// A record `id` of type `int` or `String` goes in the URL; a `List<String>` goes in the body.
class ResolveSpecClient {
  final Transport _t;

  ResolveSpecClient(String baseUrl, [ClientOptions? options])
      : _t = Transport(baseUrl, options);

  void close() => _t.close();

  static String? _urlId(Object? id) =>
      id == null || id is List ? null : id.toString();

  static List<String>? _bodyId(Object? id) =>
      id is List ? id.map((e) => e.toString()).toList() : null;

  Uri _url(String schema, String entity, String? id) {
    var u =
        '${_t.baseUrl}/${Uri.encodeComponent(schema)}/${Uri.encodeComponent(entity)}';
    if (id != null && id.isNotEmpty) u += '/${Uri.encodeComponent(id)}';
    return Uri.parse(u);
  }

  Future<Response> _send(
      String method, Uri url, Map<String, dynamic>? body) async {
    final resp = await _t.send(method, url,
        body: body == null ? null : jsonEncode(body));
    if (resp.statusCode < 200 || resp.statusCode > 299) {
      throw Transport.errorFrom(resp);
    }
    final decoded = jsonDecode(utf8.decode(resp.bodyBytes));
    final r = Response.fromJson(decoded as Map<String, dynamic>);
    if (!r.success && r.error != null) {
      throw ResolveSpecException(r.error!.message, resp.statusCode, r.error);
    }
    return r;
  }

  /// GET /{schema}/{entity}
  Future<Response> getMetadata(String schema, String entity) =>
      _send('GET', _url(schema, entity, null), null);

  Future<Response> read(String schema, String entity,
          {Object? id, Options? options}) =>
      _send(
        'POST',
        _url(schema, entity, _urlId(id)),
        {
          'operation': 'read',
          if (_bodyId(id) != null) 'id': _bodyId(id),
          if (options != null) 'options': options.toJson()
        },
      );

  Future<Response> create(String schema, String entity, Object data,
          {Options? options}) =>
      _send(
        'POST',
        _url(schema, entity, null),
        {
          'operation': 'create',
          'data': data,
          if (options != null) 'options': options.toJson()
        },
      );

  Future<Response> update(String schema, String entity, Object data,
          {Object? id, Options? options}) =>
      _send(
        'POST',
        _url(schema, entity, _urlId(id)),
        {
          'operation': 'update',
          if (_bodyId(id) != null) 'id': _bodyId(id),
          'data': data,
          if (options != null) 'options': options.toJson(),
        },
      );

  Future<Response> delete(String schema, String entity, Object id) =>
      _send('POST', _url(schema, entity, _urlId(id)), {'operation': 'delete'});
}
