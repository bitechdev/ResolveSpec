import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:resolvespec/resolvespec.dart';
import 'package:test/test.dart';

(http.Client, List<http.Request>) stub(int status, Object body, {Map<String, String> headers = const {}}) {
  final seen = <http.Request>[];
  final client = MockClient((req) async {
    seen.add(req);
    final text = body is String ? body : jsonEncode(body);
    return http.Response(text, status, headers: {'content-type': 'application/json', ...headers});
  });
  return (client, seen);
}

void main() {
  group('resolvespec', () {
    test('read posts body with headers', () async {
      final (c, seen) = stub(200, {'success': true, 'data': [{'id': 1}]});
      final client = ResolveSpecClient(
        'http://localhost:3000/',
        ClientOptions(token: 'tok', headers: {'X-Tenant': 'a'}, httpClient: c),
      );
      final r = await client.read('public', 'users',
          options: const Options(limit: 5, filters: [FilterOption('a', 'eq', 1)]));
      final req = seen.single;
      expect(req.method, 'POST');
      expect(req.url.path, '/public/users');
      expect(req.headers['authorization'], 'Bearer tok');
      expect(req.headers['x-tenant'], 'a');
      final body = jsonDecode(req.body) as Map<String, dynamic>;
      expect(body['operation'], 'read');
      expect(body['options']['limit'], 5);
      expect(body.containsKey('id'), isFalse);
      expect((r.data as List).length, 1);
    });

    test('id placement', () async {
      final (c, seen) = stub(200, {'success': true, 'data': {}});
      final client = ResolveSpecClient('http://x', ClientOptions(httpClient: c));
      await client.read('s', 'e', id: 7);
      expect(seen.last.url.path, '/s/e/7');
      await client.update('s', 'e', {'a': 1}, id: ['1', '2']);
      expect(seen.last.url.path, '/s/e');
      final b = jsonDecode(seen.last.body) as Map<String, dynamic>;
      expect(b['id'], ['1', '2']);
      expect(b['operation'], 'update');
      await client.delete('s', 'e', 'a/b');
      expect(seen.last.url.toString(), 'http://x/s/e/a%2Fb');
      expect(jsonDecode(seen.last.body)['operation'], 'delete');
    });

    test('errors', () async {
      final client = ResolveSpecClient(
        'http://x',
        ClientOptions(
            httpClient: stub(400, {
          'success': false,
          'error': {'code': 'x', 'message': 'bad', 'detail': 'why'}
        }).$1),
      );
      await expectLater(
        client.read('s', 'e'),
        throwsA(isA<ResolveSpecException>()
            .having((e) => e.statusCode, 'status', 400)
            .having((e) => e.error.code, 'code', 'x')
            .having((e) => e.message, 'message', 'bad')
            .having((e) => e.error.detail, 'detail', 'why')),
      );
      final plain = ResolveSpecClient('http://x', ClientOptions(httpClient: stub(502, 'bad gateway').$1));
      await expectLater(
        plain.read('s', 'e'),
        throwsA(isA<ResolveSpecException>().having((e) => e.message, 'message', 'bad gateway')),
      );
      final soft = ResolveSpecClient(
        'http://x',
        ClientOptions(
            httpClient: stub(200, {
          'success': false,
          'error': {'code': 'c', 'message': 'nope'}
        }).$1),
      );
      await expectLater(soft.read('s', 'e'), throwsA(isA<ResolveSpecException>().having((e) => e.message, 'message', 'nope')));
    });
  });

  group('funcspec', () {
    test('header filters', () {
      final h = buildHeaders(const FuncSpecOptions(filters: [
        FilterOption('status', 'eq', 'active'),
        FilterOption('age', 'gte', 18),
        FilterOption('name', 'contains', 'x', 'OR'),
        FilterOption('deleted', 'is_null'),
        FilterOption('id', 'in', [1, 2]),
        FilterOption('p', 'between_inclusive', [1, 5]),
      ]));
      expect(h, {
        'X-FieldFilter-status': 'active',
        'X-SearchOp-greaterthanorequal-age': '18',
        'X-SearchOr-contains-name': 'x',
        'X-SearchOp-empty-deleted': '',
        'X-SearchOp-in-id': '1,2',
        'X-SearchOp-betweeninclusive-p': '1,5',
      });
    });

    test('misc headers and encoding', () {
      var h = buildHeaders(const FuncSpecOptions(
        searchFilters: {'name': 'bob'},
        customSqlWhere: 'a = 1',
        customSqlOr: 'b = 2',
        sort: [SortOption('name', 'asc'), SortOption('created_at', 'DESC')],
        limit: 5,
        offset: 10,
        distinct: true,
        skipCount: true,
        skipCache: false,
        responseFormat: 'syncfusion',
      ));
      expect(h['X-Sort'], 'name ASC,created_at DESC');
      expect(h['X-SearchFilter-name'], 'bob');
      expect(h['X-Custom-SQL-W'], 'a = 1');
      expect(h['X-SkipCache'], 'false');
      expect(h['X-Syncfusion'], 'true');

      h = buildHeaders(const FuncSpecOptions(filters: [
        FilterOption('n', 'eq', 'héllo'),
        FilterOption('m', 'eq', ' pad'),
      ]));
      expect(h['X-FieldFilter-n'], startsWith('ZIP_'));
      expect(decodeHeaderValue(h['X-FieldFilter-n']!), 'héllo');
      expect(decodeHeaderValue(h['X-FieldFilter-m']!), ' pad');
    });

    test('query building', () {
      expect(buildQuery({'a': true, 'b': ['x', 'y'], 'c': null, 'd': 3}), {
        'a': ['true'],
        'b': ['x', 'y'],
        'd': ['3'],
      });
    });

    test('queryList metadata', () async {
      final (c, seen) = stub(206, [{'id': 1}, {'id': 2}], headers: {'Content-Range': 'items 10-12/50'});
      final client = FuncSpecClient('http://x', ClientOptions(token: 'tok', httpClient: c));
      final r = await client.queryList('/api/users', params: {'org': 1}, options: const FuncSpecOptions(limit: 2));
      expect(seen.single.method, 'GET');
      expect(seen.single.url.path, '/api/users');
      expect(seen.single.url.query, 'org=1');
      expect(seen.single.headers['x-limit'], '2');
      expect(r.metadata, const Metadata(total: 50, count: 2, filtered: 50, limit: 2, offset: 10));
      expect((r.data as List).length, 2);
    });

    test('query single and error', () async {
      final ok = FuncSpecClient('http://x', ClientOptions(httpClient: stub(200, {'id': 1}).$1));
      final r = await ok.query('api/u');
      expect(r.metadata, isNull);
      expect((r.data as Map)['id'], 1);

      final bad = FuncSpecClient(
        'http://x',
        ClientOptions(
            httpClient: stub(400, {
          'success': false,
          'error': {'code': 'hook_error', 'message': 'Hook execution failed', 'detail': 'authentication required'}
        }).$1),
      );
      await expectLater(
        bad.query('api/u'),
        throwsA(isA<ResolveSpecException>()
            .having((e) => e.error.code, 'code', 'hook_error')
            .having((e) => e.error.detail, 'detail', 'authentication required')),
      );
    });
  });
}
